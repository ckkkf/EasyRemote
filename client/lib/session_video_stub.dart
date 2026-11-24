import 'dart:ui' as ui;

/// 与 IO 版同形的结构化失败类型（Web 下恒为空）。
class AttachFailure {
  const AttachFailure({
    this.code = '',
    this.name = '',
    this.message = '画面接管失败',
    this.retryable = false,
    this.traceId = '',
  });

  final String code;
  final String name;
  final String message;
  final bool retryable;
  final String traceId;

  String get display => message;
}

/// Web 兜底：浏览器里没有原生解码与注入。
class SessionVideo {
  SessionVideo();
  static final SessionVideo instance = SessionVideo();

  /// 测试注入点（Web 无 FFI，直接完成）。
  static Future<Map<String, dynamic>> Function(Map<String, String>)
  attachRunner = (args) async => <String, dynamic>{'ok': false};

  ui.Image? get image => null;

  AttachFailure? get failure => null;
  String get error => '浏览器预览不解码视频流（需要桌面端）';
  bool get attached => false;
  int get frameCount => 0;
  int get width => 0;
  int get height => 0;
  int get presentFps => 0;
  int get nativeFps => 0;
  int get decodeMs => 0;
  int get bitrateKbps => 0;
  double get lossPct => 0;
  int get inputP95Ms => 0;
  int get inputAcks => 0;

  /// Web 无外部纹理。
  int? get textureId => null;

  Future<int?> attachTexture({required int width, required int height}) async =>
      null;

  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) async {}

  Future<void> poll() async {}

  void send(int kind, int a, [int b = 0]) {}

  void close() {}
}
