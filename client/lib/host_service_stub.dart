// Web 兜底：浏览器里没有原生采集/注入能力（dart:ffi 不可用）。
//
// UI 仍然渲染同一个「允许被控」卡片，但打开开关会得到明确提示，
// 而不是一个谁也看不懂的 NoSuchMethodError。
/// Web 同形声明：与 IO 版一致的关闭结果类型。
class HostCloseResult {
  const HostCloseResult({required this.ok, this.error = ''});

  final bool ok;
  final String error;
}

class HostStatus {
  const HostStatus({
    required this.running,
    required this.sessionId,
    required this.frames,
    required this.injected,
    this.link = '',
    required this.error,
  });

  final bool running;
  final String sessionId;
  final int frames;
  final int injected;

  /// 链路类型（与 IO 版同形，Web 下恒为空）。
  final String link;
  final String error;

  static const idle = HostStatus(
    running: false,
    sessionId: '',
    frames: 0,
    injected: 0,
    error: '',
  );

  bool get hasSession => sessionId.isNotEmpty;
}

/// 与 IO 版同形的注入点（Web 下无 FFI，注入仅用于测试占位）。
HostCloseResult Function(String, String)? _closeWorker;

set closeWorker(HostCloseResult Function(String, String)? fn) =>
    _closeWorker = fn;

/// Web 兜底：浏览器里没有原生 FFI，也不存在 worker isolate 调用。
Future<HostCloseResult> closeSessionInWorker(
  String hostId,
  String sessionId,
) async {
  final injected = _closeWorker;
  if (injected != null) return injected(hostId, sessionId);
  return const HostCloseResult(ok: false, error: '浏览器预览不支持被控');
}

class HostService {
  HostService._();
  static final HostService instance = HostService._();

  String? get password => null;
  String? get hostId => null;
  bool get active => false;

  HostCloseResult closeSession(String sessionId) =>
      const HostCloseResult(ok: false, error: '浏览器预览不支持被控');

  /// 浏览器没有原生采集/注入能力。
  static bool get supported => false;

  Never _unsupported() => throw UnsupportedError('浏览器预览不支持被控（需要桌面端）');

  String prepare({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceCode,
    required String deviceToken,
  }) => _unsupported();

  String ensurePassword() => _unsupported();

  void start({
    required String signalBase,
    int fps = 30,
    int idleTimeoutSecs = 1800,
    bool keepAwake = false,
  }) => _unsupported();

  HostStatus status() => HostStatus.idle;

  void stop() {}
}
