// Web 兜底：浏览器里没有原生采集/注入能力（dart:ffi 不可用）。
//
// UI 仍然渲染同一个「允许被控」卡片，但打开开关会得到明确提示，
// 而不是一个谁也看不懂的 NoSuchMethodError。
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

  static const idle =
      HostStatus(running: false, sessionId: '', frames: 0, injected: 0, error: '');

  bool get hasSession => sessionId.isNotEmpty;
}

class HostService {
  HostService._();
  static final HostService instance = HostService._();

  String? get password => null;
  String? get hostId => null;
  bool get active => false;

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
  }) =>
      _unsupported();

  String ensurePassword() => _unsupported();

  void start({required String signalBase, int fps = 30, int idleTimeoutSecs = 1800}) =>
      _unsupported();

  HostStatus status() => HostStatus.idle;

  void stop() {}
}
