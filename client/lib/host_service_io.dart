import 'dart:convert';
import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

/// 被控端 FFI：把「允许被控」做进同一个客户端，不再需要单独的被控程序。
///
/// 设计：客户端已登录并绑定本机设备（登录页 bindDevice），这里直接用这份会话身份
/// （token + device_id + device_code + device_token）构造被控端——**不重新登录**、
/// **不重复绑设备**，所以设备码与「我的设备」页显示的完全一致。
typedef _PrepareExC = Pointer<Utf8> Function(
    Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>);
typedef _PrepareExDart = Pointer<Utf8> Function(
    Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>, Pointer<Utf8>);

typedef _EnsurePwC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _EnsurePwDart = Pointer<Utf8> Function(Pointer<Utf8>);

typedef _StartStreamC = Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, Uint32, Uint64);
typedef _StartStreamDart = Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>, int, int);

typedef _StatusC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _StatusDart = Pointer<Utf8> Function(Pointer<Utf8>);

typedef _StopC = Int32 Function(Pointer<Utf8>);
typedef _StopDart = int Function(Pointer<Utf8>);

typedef _ReleaseC = Int32 Function(Pointer<Utf8>);
typedef _ReleaseDart = int Function(Pointer<Utf8>);

typedef _FreeStrC = Void Function(Pointer<Utf8>);
typedef _FreeStrDart = void Function(Pointer<Utf8>);

/// 被控状态（UI 轮询用）。
class HostStatus {
  const HostStatus({
    required this.running,
    required this.sessionId,
    required this.frames,
    required this.injected,
    required this.error,
  });

  final bool running;
  final String sessionId;
  final int frames;
  final int injected;
  final String error;

  static const idle = HostStatus(
      running: false, sessionId: '', frames: 0, injected: 0, error: '');

  bool get hasSession => sessionId.isNotEmpty;
}

/// 被控端服务：prepare → 密码 → 开流。
class HostService {
  HostService._();

  static final HostService instance = HostService._();

  DynamicLibrary? _lib;
  String? _hostId;
  String? _password;

  String? get password => _password;
  String? get hostId => _hostId;
  bool get active => _hostId != null;

  /// 桌面端有真实 FFI 采集/注入能力。
  static bool get supported => true;

  DynamicLibrary _load() {
    final cached = _lib;
    if (cached != null) return cached;
    final candidates = <String>[
      if (Platform.environment['ER_CORE_LIB'] != null)
        Platform.environment['ER_CORE_LIB']!,
      if (Platform.isMacOS) ...<String>[
        "${Directory(Platform.resolvedExecutable).parent.parent.path}/Frameworks/liber_ffi.dylib",
        "${Directory(Platform.resolvedExecutable).parent.parent.path}/MacOS/liber_ffi.dylib",
      ],
      if (Platform.isWindows) ...<String>[
        'er_ffi.dll',
        '../core/target/release/er_ffi.dll',
        '../core/target/debug/er_ffi.dll',
      ],
      if (!Platform.isWindows) '../core/target/debug/liber_ffi.dylib',
      if (!Platform.isWindows) '../core/target/release/liber_ffi.dylib',
    ];
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
    final free = lib.lookupFunction<_FreeStrC, _FreeStrDart>('er_free_string');
    free(p);
    return s;
  }

  /// 用当前登录会话准备主机，返回 (设备码)。
  String prepare({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceCode,
    required String deviceToken,
  }) {
    if (token.isEmpty || deviceId.isEmpty) {
      throw StateError('missing session (token/device_id)');
    }
    final lib = _load();
    final fn = lib.lookupFunction<_PrepareExC, _PrepareExDart>('er_host_prepare_ex');
    final res = _take(fn(
      gateway.toNativeUtf8(),
      relay.toNativeUtf8(),
      token.toNativeUtf8(),
      deviceId.toNativeUtf8(),
      deviceCode.toNativeUtf8(),
      deviceToken.toNativeUtf8(),
    ));
    final map = jsonDecode(res) as Map<String, dynamic>;
    if (map['ok'] != true) {
      throw StateError(map['error']?.toString() ?? 'prepare failed');
    }
    _hostId = map['host_id'] as String;
    return map['device_code'] as String? ?? deviceCode;
  }

  /// 读取/生成临时密码并同步到服务端。
  String ensurePassword() {
    final id = _hostId;
    if (id == null) throw StateError('host not prepared');
    final lib = _load();
    final fn = lib.lookupFunction<_EnsurePwC, _EnsurePwDart>('er_host_ensure_password');
    final res = _take(fn(id.toNativeUtf8()));
    final map = jsonDecode(res) as Map<String, dynamic>;
    if (map['ok'] != true) {
      throw StateError(map['error']?.toString() ?? 'password failed');
    }
    _password = map['password'] as String? ?? '';
    return _password!;
  }

  /// 开流（后台线程：等主控 → 授权 → 握手 → 推流 + 注入）。
  /// idleTimeoutSecs 是「多久没收到输入就认为主控走了」。
  ///
  /// 不能设太大：UDP 中继收不到对端离线通知，主控断开后被控端会一直空推；
  /// 之前是 1800s，导致上一个会话结束后半小时内谁都连不进来（表现就是
  /// 「已通知对方」但被控端毫无反应）。90s 是可用性与误判的折中。
  void start({required String signalBase, int fps = 30, int idleTimeoutSecs = 90}) {
    final id = _hostId;
    if (id == null) throw StateError('host not prepared');
    final lib = _load();
    final fn = lib.lookupFunction<_StartStreamC, _StartStreamDart>('er_host_start_stream');
    final res = _take(fn(id.toNativeUtf8(), signalBase.toNativeUtf8(), fps, idleTimeoutSecs));
    final map = jsonDecode(res) as Map<String, dynamic>;
    if (map['ok'] != true) {
      throw StateError(map['error']?.toString() ?? 'start failed');
    }
  }

  HostStatus status() {
    final id = _hostId;
    if (id == null) return HostStatus.idle;
    final lib = _load();
    final fn = lib.lookupFunction<_StatusC, _StatusDart>('er_host_stream_status');
    final res = _take(fn(id.toNativeUtf8()));
    try {
      final m = jsonDecode(res) as Map<String, dynamic>;
      return HostStatus(
        running: m['running'] == true,
        sessionId: m['session_id']?.toString() ?? '',
        frames: (m['frames'] as num?)?.toInt() ?? 0,
        injected: (m['injected'] as num?)?.toInt() ?? 0,
        error: m['error']?.toString() ?? '',
      );
    } catch (_) {
      return HostStatus.idle;
    }
  }

  /// 关流并释放 host（保留本地密码，下次仍是同一串）。
  void stop() {
    final id = _hostId;
    if (id == null) return;
    final lib = _load();
    lib.lookupFunction<_StopC, _StopDart>('er_host_stop_stream')(id.toNativeUtf8());
    lib.lookupFunction<_ReleaseC, _ReleaseDart>('er_host_release')(id.toNativeUtf8());
    _hostId = null;
  }
}
