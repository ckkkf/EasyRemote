import 'package:easyremote_client/host_service_stub.dart';
import 'package:easyremote_client/session_video_stub.dart';
import 'package:flutter_test/flutter_test.dart';

// ER-503a：浏览器端显式降级，不得伪装 WebTransport/视频/被控能力。
void main() {
  test('TestBrowserVideoStubIsExplicitlyDisabled', () async {
    final video = SessionVideo.instance;
    await video.attach(
      gateway: 'https://example.test',
      relay: 'relay.example.test:19000',
      token: 't',
      deviceId: 'd',
      deviceToken: 'dt',
      sessionId: 's',
    );
    expect(video.attached, isFalse);
    expect(video.frameCount, 0);
    expect(video.error, contains('桌面端'));
  });

  test('TestBrowserHostServiceIsUnsupported', () {
    expect(HostService.supported, isFalse);
  });
}
