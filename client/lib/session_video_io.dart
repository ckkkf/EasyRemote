import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:ffi/ffi.dart';
import 'package:flutter/foundation.dart';

typedef _AttachC = Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>,
    Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>);
typedef _AttachDart = Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>,
    Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>);

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
class SessionVideo {
  SessionVideo._();
  static final SessionVideo instance = SessionVideo._();

  DynamicLibrary? _lib;
  String? _ffiSession;
  ui.Image? _image;
  int _lastW = 0;
  int _lastH = 0;
  Uint8List? _buffer;
  String _error = '';
  int _frameCount = 0;

  ui.Image? get image => _image;
  String get error => _error;
  bool get attached => _ffiSession != null;
  int get frameCount => _frameCount;
  int get width => _lastW;
  int get height => _lastH;

  DynamicLibrary _load() {
    final cached = _lib;
    if (cached != null) return cached;
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
      final contents = Directory(Platform.resolvedExecutable).parent.parent.path;
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
        final lib = DynamicLibrary.open(path);
        _lib = lib;
        return lib;
      } on ArgumentError {
        continue;
      }
    }
    throw StateError('er_ffi library not found; tried ' + candidates.join(', '));
  }

  String _take(Pointer<Utf8> p) {
    if (p == nullptr) return '';
    final lib = _lib!;
    final s = p.toDartString();
    lib.lookupFunction<_FreeStrC, _FreeStrDart>('er_free_string')(p);
    return s;
  }

  /// 接管已建立的会话（不重新登录、不重复 request）。
  void attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) {
    if (_ffiSession != null) return;
    try {
      final lib = _load();
      final fn = lib.lookupFunction<_AttachC, _AttachDart>('er_ctrl_attach');
      final res = _take(fn(
        gateway.toNativeUtf8(),
        relay.toNativeUtf8(),
        token.toNativeUtf8(),
        deviceId.toNativeUtf8(),
        deviceToken.toNativeUtf8(),
        sessionId.toNativeUtf8(),
      ));
      final map = jsonDecode(res) as Map<String, dynamic>;
      if (map['ok'] != true) {
        _error = map['error']?.toString() ?? 'attach failed';
        debugPrint('[video] attach failed: ' + _error);
        return;
      }
      _ffiSession = map['session_id'] as String;
      debugPrint('[video] attached ' + _ffiSession!);
    } catch (e) {
      _error = e.toString();
      debugPrint('[video] attach exception: ' + _error);
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
      final info = jsonDecode(_take(infoFn(id.toNativeUtf8()))) as Map<String, dynamic>;
      final w = (info['width'] as num?)?.toInt() ?? 0;
      final h = (info['height'] as num?)?.toInt() ?? 0;
      if (w <= 0 || h <= 0) return;
      final need = w * h * 4;
      if (_buffer == null || _buffer!.length != need) {
        _buffer = Uint8List(need);
      }
      final bufPtr = malloc<Uint8>(need);
      try {
        final frameFn = lib.lookupFunction<_FrameC, _FrameDart>('er_ctrl_frame');
        final n = frameFn(id.toNativeUtf8(), bufPtr, need);
        if (n <= 0) return;
        _buffer!.setRange(0, n, bufPtr.asTypedList(n));
      } finally {
        malloc.free(bufPtr);
      }
      final image = await _decode(_buffer!, w, h);
      final old = _image;
      _image = image;
      _frameCount++;
      _lastW = w;
      _lastH = h;
      if (old != null) old.dispose();
    } catch (e) {
      _error = e.toString();
    }
  }

  Future<ui.Image> _decode(Uint8List rgba, int w, int h) {
    final completer = Completer<ui.Image>();
    ui.decodeImageFromPixels(rgba, w, h, ui.PixelFormat.rgba8888, completer.complete);
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
      fn(id.toNativeUtf8(), kind, a, b);
    } catch (_) {}
  }

  void close() {
    final id = _ffiSession;
    if (id == null) return;
    try {
      final lib = _lib;
      lib?.lookupFunction<_CloseC, _CloseDart>('er_ctrl_close')(id.toNativeUtf8());
    } catch (_) {}
    _ffiSession = null;
    _image?.dispose();
    _image = null;
    _frameCount = 0;
  }
}