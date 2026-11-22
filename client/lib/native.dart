import 'dart:ffi';
import 'dart:io';
import 'dart:typed_data';

import 'package:ffi/ffi.dart';

/// Rust 核心的 C ABI 绑定。
///
/// 生命周期：`NativeSession.open()` 成功后必须 `close()`；
/// 泄漏会让采集与编码器一直占用系统资源。
typedef _SessionNewC = Pointer<Void> Function();
typedef _SessionNewDart = Pointer<Void> Function();

typedef _SessionFreeC = Void Function(Pointer<Void>);
typedef _SessionFreeDart = void Function(Pointer<Void>);

typedef _ScreenSizeC = Int32 Function(
  Pointer<Void>,
  Pointer<Uint32>,
  Pointer<Uint32>,
);
typedef _ScreenSizeDart = int Function(
  Pointer<Void>,
  Pointer<Uint32>,
  Pointer<Uint32>,
);

typedef _FrameSizeC = Int32 Function(Pointer<Void>);
typedef _FrameSizeDart = int Function(Pointer<Void>);

typedef _GrabC = Int32 Function(Pointer<Void>, Pointer<Uint8>, Size);
typedef _GrabDart = int Function(Pointer<Void>, Pointer<Uint8>, int);

typedef _EncodeC = Int32 Function(Pointer<Void>, Pointer<Uint8>, Size);
typedef _EncodeDart = int Function(Pointer<Void>, Pointer<Uint8>, int);

typedef _AbrC = Pointer<Utf8> Function(Pointer<Void>, Float, Uint32, Uint32);
typedef _AbrDart = Pointer<Utf8> Function(Pointer<Void>, double, int, int);

typedef _StrC = Pointer<Utf8> Function(Pointer<Void>);
typedef _StrDart = Pointer<Utf8> Function(Pointer<Void>);

typedef _VersionC = Pointer<Utf8> Function();
typedef _VersionDart = Pointer<Utf8> Function();

typedef _FreeStrC = Void Function(Pointer<Utf8>);
typedef _FreeStrDart = void Function(Pointer<Utf8>);

/// 本机是否有硬件编码器（真开过 NVENC 会话才算）。
typedef _HwEncoderC = Int32 Function();
typedef _HwEncoderDart = int Function();

/// 当前会话实际用的编码器名："nvenc" / "software" / ""。
typedef _EncoderKindC = Pointer<Utf8> Function(Pointer<Void>);
typedef _EncoderKindDart = Pointer<Utf8> Function(Pointer<Void>);

/// 采集+编码一步到位（GPU 零拷贝优先，失败自动回退 CPU）。
typedef _GrabEncodeC = Int32 Function(Pointer<Void>, Pointer<Uint8>, Size);
typedef _GrabEncodeDart = int Function(Pointer<Void>, Pointer<Uint8>, int);

/// 当前后端名："gpu-zero-copy" / "cpu"。
typedef _BackendC = Pointer<Utf8> Function(Pointer<Void>);
typedef _BackendDart = Pointer<Utf8> Function(Pointer<Void>);

class ErNative {
  ErNative._(this._lib)
    : _new = _lib.lookupFunction<_SessionNewC, _SessionNewDart>(
        'er_session_new',
      ),
      _free = _lib.lookupFunction<_SessionFreeC, _SessionFreeDart>(
        'er_session_free',
      ),
      _screenSize = _lib.lookupFunction<_ScreenSizeC, _ScreenSizeDart>(
        'er_screen_size',
      ),
      _frameSize = _lib.lookupFunction<_FrameSizeC, _FrameSizeDart>(
        'er_frame_size',
      ),
      _grab = _lib.lookupFunction<_GrabC, _GrabDart>('er_grab_frame'),
      _encode = _lib.lookupFunction<_EncodeC, _EncodeDart>(
        'er_encode_last_frame',
      ),
      _abr = _lib.lookupFunction<_AbrC, _AbrDart>('er_abr_update'),
      _lastError = _lib.lookupFunction<_StrC, _StrDart>('er_last_error'),
      _version = _lib.lookupFunction<_VersionC, _VersionDart>('er_version'),
      _freeString = _lib.lookupFunction<_FreeStrC, _FreeStrDart>(
        'er_free_string',
      ),
      _hwEncoder = _lib.lookupFunction<_HwEncoderC, _HwEncoderDart>(
        'er_hardware_encoder_available',
      ),
      _encoderKind = _lib.lookupFunction<_EncoderKindC, _EncoderKindDart>(
        'er_encoder_kind',
      ),
      _grabAndEncode = _lib.lookupFunction<_GrabEncodeC, _GrabEncodeDart>(
        'er_grab_and_encode',
      ),
      _backend = _lib.lookupFunction<_BackendC, _BackendDart>(
        'er_encoder_backend',
      );

  final DynamicLibrary _lib;

  /// 供其他绑定复用同一个动态库句柄。
  DynamicLibrary get libHandle => _lib;
  final _SessionNewDart _new;
  final _SessionFreeDart _free;
  final _ScreenSizeDart _screenSize;
  final _FrameSizeDart _frameSize;
  final _GrabDart _grab;
  final _EncodeDart _encode;
  final _AbrDart _abr;
  final _StrDart _lastError;
  final _VersionDart _version;
  final _FreeStrDart _freeString;
  final _HwEncoderDart _hwEncoder;
  final _EncoderKindDart _encoderKind;
  final _GrabEncodeDart _grabAndEncode;
  final _BackendDart _backend;

  /// 加载动态库：环境变量 ER_CORE_LIB 优先，其次仓库相对路径。
  ///
  /// Windows 的产物名是 er_ffi.dll（MSVC cdylib），macOS 是 liber_ffi.dylib；
  /// 打包分发时 dll 与 exe 同目录，所以 Windows 也要试裸文件名（走系统 DLL 搜索路径）。
  static ErNative load() {
    final isWindows = Platform.isWindows;
    // 打包成 .app 后没有"仓库相对路径"这回事：dylib 由 CI 放进
    // Contents/Frameworks/（或 Contents/MacOS/），必须按可执行文件位置定位。
    final bundleLib = _bundleDylibPath();
    final candidates = <String>[
      if (Platform.environment['ER_CORE_LIB'] != null)
        Platform.environment['ER_CORE_LIB']!,
      if (bundleLib != null) bundleLib,
      if (isWindows) ...<String>[
        'er_ffi.dll',
        '../core/target/release/er_ffi.dll',
        '../core/target/debug/er_ffi.dll',
      ],
      if (!isWindows) '../core/target/debug/liber_ffi.dylib',
      if (!isWindows) '../core/target/release/liber_ffi.dylib',
    ];
    for (final path in candidates) {
      if (File(path).existsSync()) {
        try {
          return ErNative._(DynamicLibrary.open(path));
        } on ArgumentError {
          continue;
        }
      }
    }
    throw StateError(
      'er_ffi library not found; tried ' + candidates.join(', '),
    );
  }

  String version() {
    final p = _version();
    final s = p.toDartString();
    _freeString(p);
    return s;
  }

  NativeSession open() {
    final handle = _new();
    if (handle == nullptr) {
      throw StateError('er_session_new returned null');
    }
    return NativeSession._(this, handle);
  }

  /// 本机是否具备硬件编码（Windows/NVIDIA 才有意义）。
  bool hardwareEncoderAvailable() => _hwEncoder() == 1;

  String lastErrorOf(Pointer<Void> h) {
    final p = _lastError(h);
    final s = p.toDartString();
    _freeString(p);
    return s;
  }
}

/// 一次原生会话。
class NativeSession {
  NativeSession._(this._native, this._handle);

  final ErNative _native;
  final Pointer<Void> _handle;
  bool _closed = false;

  /// 屏幕尺寸；无显示时返回 null。
  ({int width, int height})? screenSize() {
    final w = calloc<Uint32>();
    final h = calloc<Uint32>();
    try {
      final rc = _native._screenSize(_handle, w, h);
      if (rc != 0) return null;
      return (width: w.value, height: h.value);
    } finally {
      calloc.free(w);
      calloc.free(h);
    }
  }

  /// 抓一帧 BGRA 字节。
  Uint8List grabFrame() {
    final size = _native._frameSize(_handle);
    if (size < 0) {
      throw StateError('frame size failed: ' + _native.lastErrorOf(_handle));
    }
    final buf = calloc<Uint8>(size);
    try {
      final written = _native._grab(_handle, buf, size);
      if (written < 0) {
        throw StateError(
          'grab failed (' +
              written.toString() +
              '): ' +
              _native.lastErrorOf(_handle),
        );
      }
      return Uint8List.fromList(buf.asTypedList(written));
    } finally {
      calloc.free(buf);
    }
  }

  /// 编码上一帧，返回 H.264 Annex-B 字节。
  Uint8List encodeLastFrame({int capacity = 1 << 20}) {
    final buf = calloc<Uint8>(capacity);
    try {
      final n = _native._encode(_handle, buf, capacity);
      if (n < 0) {
        throw StateError(
          'encode failed (' +
              n.toString() +
              '): ' +
              _native.lastErrorOf(_handle),
        );
      }
      return Uint8List.fromList(buf.asTypedList(n));
    } finally {
      calloc.free(buf);
    }
  }

  /// 开关隐私屏。

  /// 喂一次链路反馈，返回 ABR 决策。
  Map<String, dynamic> updateAbr({
    required double lossPct,
    required int rttMs,
    required int queueMs,
  }) {
    final p = _native._abr(_handle, lossPct, rttMs, queueMs);
    final raw = p.toDartString();
    _native._freeString(p);
    final name = RegExp('"name":"([^"]+)"').firstMatch(raw)?.group(1) ?? '';
    final bitrate =
        int.tryParse(
          RegExp('"bitrate_kbps":([0-9]+)').firstMatch(raw)?.group(1) ?? '',
        ) ??
        0;
    final changed = raw.contains('"changed":true');
    return {'name': name, 'bitrate_kbps': bitrate, 'changed': changed};
  }

  /// 采集 + 编码一步到位：优先 GPU 零拷贝，条件不满足自动回退 CPU。
  ///
  /// 与 grabFrame() + encodeLastFrame() 的区别：那条路要把整帧 BGRA 搬到 Dart 侧，
  /// 零拷贝就没意义了；这条直接在原生侧完成采集→编码。
  Uint8List grabAndEncode({int capacity = 1 << 20}) {
    final buf = calloc<Uint8>(capacity);
    try {
      final n = _native._grabAndEncode(_handle, buf, capacity);
      if (n < 0) {
        throw StateError(
          'grab+encode failed (' +
              n.toString() +
              '): ' +
              _native.lastErrorOf(_handle),
        );
      }
      return Uint8List.fromList(buf.asTypedList(n));
    } finally {
      calloc.free(buf);
    }
  }

  /// 当前采集/编码后端："gpu-zero-copy" 或 "cpu"（诊断页用）。
  String encoderBackend() {
    final p = _native._backend(_handle);
    final s = p.toDartString();
    _native._freeString(p);
    return s;
  }

  /// 当前会话实际使用的编码器："nvenc" / "software" / ""（无编码器）。
  String encoderKind() {
    final p = _native._encoderKind(_handle);
    final s = p.toDartString();
    _native._freeString(p);
    return s;
  }

  String lastError() => _native.lastErrorOf(_handle);

  void close() {
    if (_closed) return;
    _closed = true;
    _native._free(_handle);
  }
}

/// 会话编排入口（本机双端，自检与联调用）。
///
/// 单独一个类，避免和 NativeSession 的生命周期耦合：
/// 它不持有任何句柄，调用完即返回结果。
class ErSessionRunner {
  ErSessionRunner._(this._lib)
    : _runPair = _lib
          .lookupFunction<
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
            ),
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
            )
          >('er_run_pair_session'),
      _freeString = _lib
          .lookupFunction<
            Void Function(Pointer<Utf8>),
            void Function(Pointer<Utf8>)
          >('er_free_string');

  final DynamicLibrary _lib;

  /// 供其他绑定复用同一个动态库句柄。
  DynamicLibrary get libHandle => _lib;
  final Pointer<Utf8> Function(
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
  )
  _runPair;
  final void Function(Pointer<Utf8>) _freeString;

  static ErSessionRunner load() {
    final native = ErNative.load();
    return ErSessionRunner._(native.libHandle);
  }

  /// 跑一次完整会话，返回解析后的 JSON（失败时 ok=false 且带 error）。
  Map<String, dynamic> run({
    required String gatewayUrl,
    required String relayAddr,
    required String phoneA,
    required String phoneB,
    String smsCode = '123456',
  }) {
    final args = [
      gatewayUrl,
      relayAddr,
      phoneA,
      phoneB,
      smsCode,
    ].map((s) => s.toNativeUtf8()).toList();
    try {
      final p = _runPair(args[0], args[1], args[2], args[3], args[4]);
      final raw = p.toDartString();
      _freeString(p);
      return parseFlatJson(raw);
    } finally {
      for (final a in args) {
        calloc.free(a);
      }
    }
  }

  /// 解析扁平 JSON（本库只返回一层，手写解析避免引入依赖）。
  static Map<String, dynamic> parseFlatJson(String raw) {
    final out = <String, dynamic>{};
    final re = RegExp(r'"([a-z_]+)":s*("([^"]*)"|true|false|-?[0-9]+)');
    for (final m in re.allMatches(raw)) {
      final key = m.group(1)!;
      final quoted = m.group(3);
      final token = m.group(2)!;
      if (quoted != null) {
        out[key] = quoted;
      } else if (token == 'true') {
        out[key] = true;
      } else if (token == 'false') {
        out[key] = false;
      } else {
        out[key] = int.tryParse(token) ?? token;
      }
    }
    return out;
  }
}

/// 主机 / 主控两段式会话（v2：主控只用设备码 + 验证码）。
///
/// 被控端通过信令自行发现会话；主控不需要知道 host_id。
class ErHostLink {
  ErHostLink._(DynamicLibrary lib)
    : _prepare = lib
          .lookupFunction<
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
            ),
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
            )
          >('er_host_prepare'),
      _serve = lib
          .lookupFunction<
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Uint32,
              Uint32,
            ),
            Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, int, int)
          >('er_host_serve'),
      _connect = lib
          .lookupFunction<
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Uint32,
            ),
            Pointer<Utf8> Function(
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              Pointer<Utf8>,
              int,
            )
          >('er_device_connect'),
      _release = lib
          .lookupFunction<
            Int32 Function(Pointer<Utf8>),
            int Function(Pointer<Utf8>)
          >('er_host_release'),
      _freeString = lib
          .lookupFunction<
            Void Function(Pointer<Utf8>),
            void Function(Pointer<Utf8>)
          >('er_free_string');

  final Pointer<Utf8> Function(
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
  )
  _prepare;
  final Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, int, int) _serve;
  final Pointer<Utf8> Function(
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    Pointer<Utf8>,
    int,
  )
  _connect;
  final int Function(Pointer<Utf8>) _release;
  final void Function(Pointer<Utf8>) _freeString;

  static ErHostLink load() => ErHostLink._(ErNative.load().libHandle);

  /// 准备主机，返回 {ok, host_id, device_code, captcha}。
  Map<String, dynamic> prepare({
    required String gatewayUrl,
    required String relayAddr,
    required String phone,
    String smsCode = '123456',
  }) {
    final args = _args([gatewayUrl, relayAddr, phone, smsCode]);
    try {
      return ErSessionRunner.parseFlatJson(
        _call(() => _prepare(args[0], args[1], args[2], args[3])),
      );
    } finally {
      _free(args);
    }
  }

  /// 主机侧服务：阻塞等待信令并收帧。**必须在后台 isolate 调用**，否则卡 UI。
  Map<String, dynamic> serve({
    required String hostId,
    required String signalBase,
    int frames = 5,
    int timeoutSecs = 30,
  }) {
    final args = _args([hostId, signalBase]);
    try {
      return ErSessionRunner.parseFlatJson(
        _call(() => _serve(args[0], args[1], frames, timeoutSecs)),
      );
    } finally {
      _free(args);
    }
  }

  /// 主控连接：设备码 + 验证码。
  Map<String, dynamic> connect({
    required String gatewayUrl,
    required String relayAddr,
    required String phone,
    required String deviceCode,
    required String captcha,
    String smsCode = '123456',
    int frames = 5,
  }) {
    final args = _args([
      gatewayUrl,
      relayAddr,
      phone,
      deviceCode,
      captcha,
      smsCode,
    ]);
    try {
      return ErSessionRunner.parseFlatJson(
        _call(
          () => _connect(
            args[0],
            args[1],
            args[2],
            args[3],
            args[4],
            args[5],
            frames,
          ),
        ),
      );
    } finally {
      _free(args);
    }
  }

  bool release(String hostId) {
    final p = hostId.toNativeUtf8();
    try {
      return _release(p) == 1;
    } finally {
      calloc.free(p);
    }
  }

  List<Pointer<Utf8>> _args(List<String> values) =>
      values.map((s) => s.toNativeUtf8()).toList();

  void _free(List<Pointer<Utf8>> args) {
    for (final a in args) {
      calloc.free(a);
    }
  }

  String _call(Pointer<Utf8> Function() fn) {
    final p = fn();
    final raw = p.toDartString();
    _freeString(p);
    return raw;
  }
}

/// macOS .app 包内的 Rust 核心库位置（CI 打包时拷进去）。
///
/// 可执行文件在 Contents/MacOS/Runner，库放在 Contents/Frameworks/liber_ffi.dylib；
/// 也兼容直接和可执行文件同级（Contents/MacOS/）。
String? _bundleDylibPath() {
  if (!Platform.isMacOS) return null;
  try {
    final exe = Platform.resolvedExecutable;
    final contents = Directory(exe).parent.parent.path;
    for (final p in <String>[
      "$contents/Frameworks/liber_ffi.dylib",
      "$contents/MacOS/liber_ffi.dylib",
      "$contents/Resources/liber_ffi.dylib",
    ]) {
      if (File(p).existsSync()) return p;
    }
  } catch (_) {}
  return null;
}
