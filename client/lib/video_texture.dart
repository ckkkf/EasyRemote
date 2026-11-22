import 'package:flutter/services.dart';

/// ER-601b：native 外部纹理桥。平台不支持时返回 null，调用方回退 RGBA。
class VideoTextureBridge {
  static const MethodChannel _channel = MethodChannel(
    'easyremote/video_texture',
  );

  static Future<int?> create({
    required String sessionId,
    required int width,
    required int height,
  }) async {
    try {
      final id = await _channel.invokeMethod<int>('create', {
        'session_id': sessionId,
        'width': width,
        'height': height,
      });
      return id;
    } on MissingPluginException {
      return null;
    } on PlatformException {
      return null;
    }
  }

  static Future<void> dispose(int textureId) async {
    try {
      await _channel.invokeMethod<void>('dispose', {'texture_id': textureId});
    } on MissingPluginException {
      return;
    } on PlatformException {
      return;
    }
  }
}
