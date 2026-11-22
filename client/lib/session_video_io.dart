import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:isolate';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:ffi/ffi.dart';
import 'package:flutter/foundation.dart';

import 'present_fps.dart';
import 'video_texture.dart';

typedef _AttachC = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);
typedef _AttachDart = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);

typedef _InfoC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _InfoDart = Pointer<Utf8> Function(Pointer<Utf8>);

typedef _FrameC = Int32 Function(Pointer<Utf8>, Pointer<Uint8>, Size);
typedef _FrameDart = int Function(Pointer<Utf8>, Pointer<Uint8>, int);

typedef _InputC = Int32 Function(Pointer<Utf8>, Int32, Int32, Int32);
typedef _InputDart = int Function(Pointer<Utf8>, int, int, int);

typedef _CloseC = Int32 Function(Pointer<Utf8>);
typedef _CloseDart = int Function(Pointer<Utf8>);

typedef _FreeStrC = Void Function(Pointer<Utf8>);
typedef _FreeStrDart = void Function(Pointer<Utf8>);

/// 会话画面：FFI 收帧 → 解码 → 给 UI 一个可绘制的 ui.Image。
///
/// 为什么是「拉」而不是「推」：Dart 侧定时轮询 poll()，Rust 后台线程只管把最新帧
/// 放进双缓冲。UI 卡顿不会堆积解码，也不会因为回调线程不同步而崩。
typedef AttachRunner = Future<Map<String, dynamic>> Function(
  Map<String, String> args,
);

class SessionVideo {
  SessionVideo._();
  static final SessionVideo instance = SessionVideo._();

  /// 测试注入点：默认在后台 isolate 执行阻塞 attach。
  static AttachRunner attachRunner = _attachInIsolate;

  DynamicLibrary? _lib;
  String? _ffiSession;
  ui.Image? _image;
  int _lastW = 0;
  int _lastH = 0;
  Uint8List? _buffer;
  String _error = '';
  int _frameCount = 0;
  int _nativeFps = 0;
  int _decodeMs = 0;
  int _dropped = 0;
  int _bitrateKbps = 0;
  double _lossPct = 0;
  int _inputP95Ms = 0;
  int _inputAcks = 0;
  final PresentFps _present = PresentFps();
  int? _textureId;

  ui.Image? get image => _image;
  String get error => _error;
  bool get attached => _ffiSession != null;
  int get frameCount => _frameCount;

  /// native 侧最近一秒解码 FPS（由 er_ctrl_info 上报）。
  int get nativeFps => _nativeFps;
  int get decodeMs => _decodeMs;
  int get droppedFrames => _dropped;
  int get bitrateKbps => _bitrateKbps;
  double get lossPct => _lossPct;
  int get inputP95Ms => _inputP95Ms;
  int get inputAcks => _inputAcks;

  /// UI 实际呈现帧率（与 native decode_fps 对照）。
  int get presentFps => _present.fps;

  /// native 外部纹理 id；null 表示回退 RGBA/ui.Image。
  int? get textureId => _textureId;

  /// 把当前 native 帧交给 Flutter 纹理；平台不支持返回 null。
  Future<int?> attachTexture({required int width, required int height}) async {
    final id = _ffiSession;
    if (id == null) return null;
    _textureId = await VideoTextureBridge.create(
      sessionId: id,
      width: width,
      height: height,
    );
    return _textureId;
  }

  int get width => _lastW;
  int get height => _lastH;

  /// 在任意 isolate 打开动态库（FFI 句柄可跨 isolate 重新加载）。
  static DynamicLibrary openLibrary() {
    final candidates = <String>[
      if (Platform.environment['ER_CORE_LIB'] != null)
        Platform.environment['ER_CORE_LIB']!,
      if (Platform.isWindows) ...<String>[
        'er_ffi.dll',
        '../core/target/release/er_ffi.dll',
        '../core/target/debug/er_ffi.dll',
      ],
    ];
    if (Platform.isMacOS) {
      final contents = Directory(Platform.resolvedExecutable)
          .parent
          .parent
          .path;
      candidates.add(contents + '/Frameworks/liber_ffi.dylib');
      candidates.add(contents + '/MacOS/liber_ffi.dylib');
    }
    if (!Platform.isWindows) {
      candidates.add('../core/target/debug/liber_ffi.dylib');
      candidates.add('../core/target/release/liber_ffi.dylib');
    }
    for (final path in candidates) {
      if (!Platform.isWindows && !File(path).existsSync()) continue;
      try {
        return DynamicLibrary.open(path);
      } on ArgumentError {
        continue;
      }
    }
    throw StateError(
      'er_ffi library not found; tried ' + candidates.join(', '),
    );
  }

  String _take(Pointer<Utf8> p) {
    if (p == nullptr) return '';
    final lib = _lib!;
    final s = p.toDartString();
    lib.lookupFunction<_FreeStrC, _FreeStrDart>('er_free_string')(p);
    return s;
  }

  /// 接管已建立的会话（不重新登录、不重复 request）。
  ///
  /// 握手/建流是阻塞网络调用，必须放在后台 isolate：放在 UI isolate 会让
  /// 画面切换、输入和心跳一起卡住。FFI 会话句柄在进程内共享，isolate 返回
  /// session_id 后主 isolate 继续 poll/send/close。
  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) async {
    if (_ffiSession != null) return;
    try {
      final map = await attachRunner({
        'gateway': gateway,
        'relay': relay,
        'token': token,
        'device_id': deviceId,
        'device_token': deviceToken,
        'session_id': sessionId,
      });
      if (map['ok'] != true) {
        _error = map['error']?.toString() ?? 'attach failed';
        debugPrint('[video] attach failed: $_error');
        return;
      }
      _lib ??= openLibrary();
      _ffiSession = map['session_id'] as String;
      debugPrint('[video] attached $_ffiSession');
    } catch (e) {
      _error = e.toString();
      debugPrint('[video] attach exception: $_error');
    }
  }

  /// 取一帧并解码成 ui.Image。UI 每 60ms 调一次即可。
  Future<void> poll() async {
    final id = _ffiSession;
    if (id == null) return;
    final lib = _lib;
    if (lib == null) return;
    try {
      final infoFn = lib.lookupFunction<_InfoC, _InfoDart>('er_ctrl_info');
      final idPtr = id.toNativeUtf8();
      final String infoRaw;
      try {
        infoRaw = _take(infoFn(idPtr));
      } finally {
        calloc.free(idPtr);
      }
      final info = jsonDecode(infoRaw) as Map<String, dynamic>;
      _nativeFps = (info['fps'] as num?)?.toInt() ?? _nativeFps;
      _decodeMs = (info['decode_ms'] as num?)?.toInt() ?? _decodeMs;
      _dropped = (info['dropped'] as num?)?.toInt() ?? _dropped;
      _bitrateKbps = (info['bitrate_kbps'] as num?)?.toInt() ?? _bitrateKbps;
      _lossPct = (info['loss_pct'] as num?)?.toDouble() ?? _lossPct;
      _inputP95Ms = (info['input_p95_ms'] as num?)?.toInt() ?? _inputP95Ms;
      _inputAcks = (info['input_acks'] as num?)?.toInt() ?? _inputAcks;
      final w = (info['width'] as num?)?.toInt() ?? 0;
      final h = (info['height'] as num?)?.toInt() ?? 0;
      if (w <= 0 || h <= 0) return;
      final need = w * h * 4;
      if (_buffer == null || _buffer!.length != need) {
        _buffer = Uint8List(need);
      }
      final bufPtr = malloc<Uint8>(need);
      try {
        final frameFn = lib.lookupFunction<_FrameC, _FrameDart>(
          'er_ctrl_frame',
        );
        final sidPtr = id.toNativeUtf8();
        final int n;
        try {
          n = frameFn(sidPtr, bufPtr, need);
        } finally {
          calloc.free(sidPtr);
        }
        if (n <= 0) return;
        _buffer!.setRange(0, n, bufPtr.asTypedList(n));
      } finally {
        malloc.free(bufPtr);
      }
      final image = await _decode(_buffer!, w, h);
      final old = _image;
      _image = image;
      _frameCount++;
      _present.tick();
      _lastW = w;
      _lastH = h;
      if (old != null) old.dispose();
    } catch (e) {
      _error = e.toString();
    }
  }

  Future<ui.Image> _decode(Uint8List rgba, int w, int h) {
    final completer = Completer<ui.Image>();
    ui.decodeImageFromPixels(
      rgba,
      w,
      h,
      ui.PixelFormat.rgba8888,
      completer.complete,
    );
    return completer.future;
  }

  /// 发输入。kind: 0=move 1=button 2=wheel 3=key
  void send(int kind, int a, [int b = 0]) {
    final id = _ffiSession;
    if (id == null) return;
    final lib = _lib;
    if (lib == null) return;
    try {
      final fn = lib.lookupFunction<_InputC, _InputDart>('er_ctrl_input');
      final sidPtr = id.toNativeUtf8();
      try {
        fn(sidPtr, kind, a, b);
      } finally {
        calloc.free(sidPtr);
      }
    } catch (e) {
      debugPrint('[video] input failed: $e');
    }
  }

  void close() {
    final id = _ffiSession;
    if (id == null) return;
    try {
      final lib = _lib;
      final sidPtr = id.toNativeUtf8();
      try {
        lib?.lookupFunction<_CloseC, _CloseDart>('er_ctrl_close')(sidPtr);
      } finally {
        calloc.free(sidPtr);
      }
    } catch (e) {
      debugPrint('[video] close failed: $e');
    }
    final texture = _textureId;
    if (texture != null) {
      _textureId = null;
      // 异步释放，不能阻塞 close。
      unawaited(VideoTextureBridge.dispose(texture));
    }
    _ffiSession = null;
    _image?.dispose();
    _image = null;
    _frameCount = 0;
  }
}

/// 后台 isolate 里执行真正的 attach，并把 native 字符串全部释放。
Map<String, dynamic> _attachNative(Map<String, String> a) {
  Pointer<Utf8>? pGateway;
  Pointer<Utf8>? pRelay;
  Pointer<Utf8>? pToken;
  Pointer<Utf8>? pDevice;
  Pointer<Utf8>? pDeviceToken;
  Pointer<Utf8>? pSession;
  try {
    final lib = SessionVideo.openLibrary();
    final fn = lib.lookupFunction<_AttachC, _AttachDart>('er_ctrl_attach');
    final free = lib.lookupFunction<_FreeStrC, _FreeStrDart>('er_free_string');
    pGateway = a['gateway']!.toNativeUtf8();
    pRelay = a['relay']!.toNativeUtf8();
    pToken = a['token']!.toNativeUtf8();
    pDevice = a['device_id']!.toNativeUtf8();
    pDeviceToken = a['device_token']!.toNativeUtf8();
    pSession = a['session_id']!.toNativeUtf8();
    final rawPtr = fn(
      pGateway,
      pRelay,
      pToken,
      pDevice,
      pDeviceToken,
      pSession,
    );
    if (rawPtr == nullptr) {
      return <String, dynamic>{'ok': false, 'error': 'null result'};
    }
    final raw = rawPtr.toDartString();
    free(rawPtr);
    return jsonDecode(raw) as Map<String, dynamic>;
  } catch (e) {
    return <String, dynamic>{'ok': false, 'error': e.toString()};
  } finally {
    for (final p in [
      pGateway,
      pRelay,
      pToken,
      pDevice,
      pDeviceToken,
      pSession,
    ]) {
      if (p != null) calloc.free(p);
    }
  }
}

Future<Map<String, dynamic>> _attachInIsolate(Map<String, String> args) =>
    Isolate.run(() => _attachNative(args));
