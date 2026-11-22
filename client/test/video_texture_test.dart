import 'package:easyremote_client/video_texture.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('TestTextureBridgeFallsBackWithoutPlugin', () async {
    final id = await VideoTextureBridge.create(
      sessionId: 's',
      width: 640,
      height: 360,
    );
    expect(id, isNull, reason: '测试环境没有平台插件，必须回退而不是抛错');
    await VideoTextureBridge.dispose(0);
  });
}
