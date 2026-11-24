import 'dart:ui' as ui;

import 'session_video.dart';

/// SessionPage 使用的视频会话抽象（ER-905 4.1(4)）。
///
/// 为什么要这层：widget 测试不能加载真实 FFI，但必须验证**页面**行为
/// （错误文案脱敏、trace_id 复制按钮、重连真的再次 attach、并发不重复）。
/// 每个会话页独占 native 画面句柄，迟到的结果不会覆盖下一次会话。
abstract class VideoAdapter {
  ui.Image? get image;
  AttachFailure? get failure;
  String get error;
  bool get attached;

  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  });

  void send(int kind, int a, [int b = 0]);
  Future<void> poll();
  void close();
}

/// 生产实现：使用页面所属的 SessionVideo。
class DefaultVideoAdapter implements VideoAdapter {
  DefaultVideoAdapter();

  final SessionVideo session = SessionVideo();

  @override
  ui.Image? get image => session.image;

  @override
  AttachFailure? get failure => session.failure;

  @override
  String get error => session.error;

  @override
  bool get attached => session.attached;

  @override
  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) => session.attach(
    gateway: gateway,
    relay: relay,
    token: token,
    deviceId: deviceId,
    deviceToken: deviceToken,
    sessionId: sessionId,
  );

  @override
  void send(int kind, int a, [int b = 0]) => session.send(kind, a, b);

  @override
  Future<void> poll() => session.poll();

  @override
  void close() => session.close();
}
