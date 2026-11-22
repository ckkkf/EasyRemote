import 'dart:async';

import 'package:easyremote_client/session_video.dart';
import 'package:flutter_test/flutter_test.dart';

// ER-402：attach 阻塞时 UI 心跳/定时器必须继续跑；默认实现把 attach 放后台 isolate。
void main() {
  test('TestAttachDoesNotBlockUiHeartbeat', () async {
    final gate = Completer<Map<String, dynamic>>();
    final previous = SessionVideo.attachRunner;
    SessionVideo.attachRunner = (_) => gate.future;
    addTearDown(() => SessionVideo.attachRunner = previous);

    var ticks = 0;
    final heartbeat = Timer.periodic(const Duration(milliseconds: 10), (_) {
      ticks++;
    });
    addTearDown(heartbeat.cancel);

    final attaching = SessionVideo.instance.attach(
      gateway: 'http://gw.test',
      relay: '127.0.0.1:1',
      token: 't',
      deviceId: 'd',
      deviceToken: 'dt',
      sessionId: 's',
    );

    await Future<void>.delayed(const Duration(milliseconds: 60));
    expect(ticks, greaterThanOrEqualTo(2), reason: 'attach 未完成时事件循环必须继续运行');

    gate.complete(<String, dynamic>{'ok': false, 'error': 'test'});
    await attaching;
  });

  test('TestManualReconnectReattaches', () async {
    final previous = SessionVideo.attachRunner;
    var calls = 0;
    SessionVideo.attachRunner = (_) async {
      calls++;
      return <String, dynamic>{'ok': false, 'error': 'test'};
    };
    addTearDown(() => SessionVideo.attachRunner = previous);

    Future<void> attach() => SessionVideo.instance.attach(
      gateway: 'http://gw.test',
      relay: '127.0.0.1:1',
      token: 't',
      deviceId: 'd',
      deviceToken: 'dt',
      sessionId: 's',
    );

    await attach();
    SessionVideo.instance.close();
    await attach();
    expect(calls, 2, reason: '手动重连必须重新发起 attach');
    expect(SessionVideo.instance.attached, isFalse);
  });
}
