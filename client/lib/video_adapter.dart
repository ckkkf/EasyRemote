import 'dart:ui' as ui;

import 'session_video.dart';

/// SessionPage 使用的视频会话抽象（ER-905 4.1(4)）。
///
/// 为什么要这层：widget 测试不能加载真实 FFI，但必须验证**页面**行为
/// （错误文案脱敏、trace_id 复制按钮、重连真的再次 attach、并发不重复）。
/// 生产默认实现只是把调用转给 SessionVideo.instance，行为不变。
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

  Future<void> poll();
  void close();
}

/// 生产实现：委托全局 SessionVideo 单例。
class DefaultVideoAdapter implements VideoAdapter {
  const DefaultVideoAdapter();

  @override
  ui.Image? get image => SessionVideo.instance.image;

  @override
  AttachFailure? get failure => SessionVideo.instance.failure;

  @override
  String get error => SessionVideo.instance.error;

  @override
  bool get attached => SessionVideo.instance.attached;

  @override
  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) => SessionVideo.instance.attach(
    gateway: gateway,
    relay: relay,
    token: token,
    deviceId: deviceId,
    deviceToken: deviceToken,
    sessionId: sessionId,
  );

  @override
  Future<void> poll() => SessionVideo.instance.poll();

  @override
  void close() => SessionVideo.instance.close();
}
