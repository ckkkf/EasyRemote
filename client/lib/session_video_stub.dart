import 'dart:ui' as ui;

/// Web 兜底：浏览器里没有原生解码与注入。
class SessionVideo {
  SessionVideo._();
  static final SessionVideo instance = SessionVideo._();

  ui.Image? get image => null;
  String get error => '浏览器预览不解码视频流（需要桌面端）';
  bool get attached => false;
  int get frameCount => 0;
  int get width => 0;
  int get height => 0;

  void attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) {}

  Future<void> poll() async {}

  void send(int kind, int a, [int b = 0]) {}

  void close() {}
}