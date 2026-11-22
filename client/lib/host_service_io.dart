import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

import 'capabilities.dart';
import 'consent_controller.dart';
import 'host_policy.dart';
import 'prefs.dart';

/// 被控端 FFI：把「允许被控」做进同一个客户端，不再需要单独的被控程序。
///
/// 设计：客户端已登录并绑定本机设备（登录页 bindDevice），这里直接用这份会话身份
/// （token + device_id + device_code + device_token）构造被控端——**不重新登录**、
/// **不重复绑设备**，所以设备码与「我的设备」页显示的完全一致。
typedef _PrepareExC = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);
typedef _PrepareExDart = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
  Pointer<Utf8>,
);

typedef _EnsurePwC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _EnsurePwDart = Pointer<Utf8> Function(Pointer<Utf8>);

typedef _StartStreamC = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Uint32,
  Uint64,
  Int32,
);
typedef _StartStreamDart = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  int,
  int,
  int,
);

typedef _StatusC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _StatusDart = Pointer<Utf8> Function(Pointer<Utf8>);

typedef _StopC = Int32 Function(Pointer<Utf8>);
typedef _StopDart = int Function(Pointer<Utf8>);

typedef _ReleaseC = Int32 Function(Pointer<Utf8>);
typedef _ReleaseDart = int Function(Pointer<Utf8>);

typedef _FreeStrC = Void Function(Pointer<Utf8>);
typedef _FreeStrDart = void Function(Pointer<Utf8>);

typedef _CloseSessionC = Pointer<Utf8> Function(Pointer<Utf8>, Pointer<Utf8>);
typedef _CloseSessionDart = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
);
typedef _PendingConsentC = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _PendingConsentDart = Pointer<Utf8> Function(Pointer<Utf8>);
typedef _DecideConsentC = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  Int32,
);
typedef _DecideConsentDart = Pointer<Utf8> Function(
  Pointer<Utf8>,
  Pointer<Utf8>,
  int,
);

/// 被控状态（UI 轮询用）。
class HostStatus {
  const HostStatus({
    required this.running,
    required this.sessionId,
    required this.frames,
    required this.injected,
    required this.link,
    required this.error,
  });

  final bool running;
  final String sessionId;
  final int frames;
  final int injected;
  final String link;
  final String error;

  static const idle = HostStatus(
    running: false,
    sessionId: '',
    frames: 0,
    injected: 0,
    link: '',
    error: '',
  );

  bool get hasSession => sessionId.isNotEmpty;
}

/// 被控端服务：prepare → 密码 → 开流。
class HostService {
  HostService._();

  static final HostService instance = HostService._();

  DynamicLibrary? _lib;
  String? _hostId;
  String? _password;
  Timer? _consentPoll;
  StreamSubscription<ConsentDecision>? _decisionSub;

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
    throw StateError(
      'er_ffi library not found; tried ' + candidates.join(', '),
    );
  }

  /// 真正调 FFI 前的最后一道门禁：偏好关闭或能力未接入都直接拒绝。
  void _assertHostAllowed() {
    final reason = hostBlockReason(
      capabilityAvailable: CapabilityGate.instance.isAvailable('remote_host'),
      prefs: appPrefs.value,
    );
    if (reason != null) throw StateError('被控未启动：$reason');
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
    _assertHostAllowed();
    if (token.isEmpty || deviceId.isEmpty) {
      throw StateError('missing session (token/device_id)');
    }
    final lib = _load();
    final fn = lib.lookupFunction<_PrepareExC, _PrepareExDart>(
      'er_host_prepare_ex',
    );
    final pg = gateway.toNativeUtf8();
    final pr = relay.toNativeUtf8();
    final pt = token.toNativeUtf8();
    final pd = deviceId.toNativeUtf8();
    final pc = deviceCode.toNativeUtf8();
    final pdt = deviceToken.toNativeUtf8();
    final String res;
    try {
      res = _take(fn(pg, pr, pt, pd, pc, pdt));
    } finally {
      for (final p in [pg, pr, pt, pd, pc, pdt]) {
        calloc.free(p);
      }
    }
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
    final fn = lib.lookupFunction<_EnsurePwC, _EnsurePwDart>(
      'er_host_ensure_password',
    );
    final hid = id.toNativeUtf8();
    final String res;
    try {
      res = _take(fn(hid));
    } finally {
      calloc.free(hid);
    }
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
  void start({
    required String signalBase,
    int fps = 30,
    int idleTimeoutSecs = 90,
    bool keepAwake = false,
  }) {
    _assertHostAllowed();
    final id = _hostId;
    if (id == null) throw StateError('host not prepared');
    final lib = _load();
    final fn = lib.lookupFunction<_StartStreamC, _StartStreamDart>(
      'er_host_start_stream',
    );
    final hid = id.toNativeUtf8();
    final psig = signalBase.toNativeUtf8();
    final String res;
    try {
      res = _take(fn(hid, psig, fps, idleTimeoutSecs, keepAwake ? 1 : 0));
    } finally {
      calloc.free(hid);
      calloc.free(psig);
    }
    final map = jsonDecode(res) as Map<String, dynamic>;
    if (map['ok'] != true) {
      throw StateError(map['error']?.toString() ?? 'start failed');
    }
    _startConsentBridge();
  }

  /// 轮询 Rust 侧的本地授权请求，喂给 ConsentController；决策再回传 FFI。
  /// 只有 UI 调用 accept/reject 才会产生决策，这里不自动同意。
  void _startConsentBridge() {
    _consentPoll?.cancel();
    _decisionSub ??= ConsentController.instance.decisions.listen((d) {
      final id = _hostId;
      if (id != null) decideConsent(d.sessionId, d.accepted);
    });
    _consentPoll = Timer.periodic(
      const Duration(milliseconds: 500),
      (_) => _pollConsent(),
    );
    _pollConsent();
  }

  void _pollConsent() {
    final id = _hostId;
    if (id == null) return;
    final pending = pendingConsent();
    if (pending == null) return;
    final sessionId = pending['session_id'] as String? ?? '';
    if (sessionId.isEmpty) return;
    if (ConsentController.instance.pending?.sessionId == sessionId) return;
    ConsentController.instance.request(
      sessionId: sessionId,
      controllerLabel: pending['controller_label'] as String? ?? '远程设备',
    );
  }

  /// 读取等待本机决策的会话（null 表示没有）。
  Map<String, dynamic>? pendingConsent() {
    final id = _hostId;
    if (id == null) return null;
    final lib = _load();
    final fn = lib.lookupFunction<_PendingConsentC, _PendingConsentDart>(
      'er_host_pending_consent',
    );
    final hid = id.toNativeUtf8();
    final String raw;
    try {
      raw = _take(fn(hid));
    } finally {
      calloc.free(hid);
    }
    final map = jsonDecode(raw) as Map<String, dynamic>;
    return map['ok'] == true ? map : null;
  }

  /// 本机断开当前会话（本地控制条）。返回 false 表示失败。
  bool closeSession(String sessionId) {
    final id = _hostId;
    if (id == null || sessionId.isEmpty) return false;
    final lib = _load();
    final fn = lib.lookupFunction<_CloseSessionC, _CloseSessionDart>(
      'er_host_close_session',
    );
    final hid = id.toNativeUtf8();
    final sid = sessionId.toNativeUtf8();
    final String raw;
    try {
      raw = _take(fn(hid, sid));
    } finally {
      calloc.free(hid);
      calloc.free(sid);
    }
    final map = jsonDecode(raw) as Map<String, dynamic>;
    return map['ok'] == true;
  }

  /// 本机决策（accept/reject）；false 表示没有匹配的 pending。
  bool decideConsent(String sessionId, bool accepted) {
    final id = _hostId;
    if (id == null) return false;
    final lib = _load();
    final fn = lib.lookupFunction<_DecideConsentC, _DecideConsentDart>(
      'er_host_decide_consent',
    );
    final hid = id.toNativeUtf8();
    final sid = sessionId.toNativeUtf8();
    final String raw;
    try {
      raw = _take(fn(hid, sid, accepted ? 1 : 0));
    } finally {
      calloc.free(hid);
      calloc.free(sid);
    }
    final map = jsonDecode(raw) as Map<String, dynamic>;
    return map['ok'] == true;
  }

  HostStatus status() {
    final id = _hostId;
    if (id == null) return HostStatus.idle;
    final lib = _load();
    final fn = lib.lookupFunction<_StatusC, _StatusDart>(
      'er_host_stream_status',
    );
    final hid = id.toNativeUtf8();
    final String res;
    try {
      res = _take(fn(hid));
    } finally {
      calloc.free(hid);
    }
    try {
      final m = jsonDecode(res) as Map<String, dynamic>;
      return HostStatus(
        running: m['running'] == true,
        sessionId: m['session_id']?.toString() ?? '',
        frames: (m['frames'] as num?)?.toInt() ?? 0,
        injected: (m['injected'] as num?)?.toInt() ?? 0,
        link: m['link']?.toString() ?? '',
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
    _consentPoll?.cancel();
    _consentPoll = null;
    final lib = _load();
    final hid = id.toNativeUtf8();
    try {
      lib.lookupFunction<_StopC, _StopDart>('er_host_stop_stream')(hid);
      lib.lookupFunction<_ReleaseC, _ReleaseDart>('er_host_release')(hid);
    } finally {
      calloc.free(hid);
    }
    _hostId = null;
  }
}
