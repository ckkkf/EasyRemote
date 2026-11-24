import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/session_video.dart';

/// 设一个一定会失败的 attach 结果（结构化），返回用于断言的状态。
Future<void> attachWith(Map<String, dynamic> result) async {
  SessionVideo.attachRunner = (args) async => result;
  await SessionVideo.instance.attach(
    gateway: 'http://127.0.0.1:18090',
    relay: '127.0.0.1:19000',
    token: 't',
    deviceId: 'dev-1',
    deviceToken: 'dt',
    sessionId: 'ses-1',
  );
}

void main() {
  tearDown(() {
    SessionVideo.instance.close();
  });

  test('TestAttachFailureKeepsStructuredFieldsWithoutInternalUrl', () async {
    await attachWith({
      'ok': false,
      'error': {
        'code': '300006',
        'name': 'SESSION_AUTH_WINDOW_EXCEEDED',
        'message': '本次授权已到期，请重新连接',
        'retryable': false,
        'trace_id': 'tr-abc',
      },
    });

    final f = SessionVideo.instance.failure;
    expect(f, isNotNull);
    expect(f!.traceId, 'tr-abc');
    expect(f.name, 'SESSION_AUTH_WINDOW_EXCEEDED');
    expect(f.message, '本次授权已到期，请重新连接');
    // UI 文案不得泄露内部细节
    expect(f.display.contains('http://'), isFalse);
    expect(f.display.contains('POST'), isFalse);
    expect(f.display.contains('body='), isFalse);
    expect(f.display.contains('{'), isFalse, reason: '不得显示原始 JSON');
  });

  test('TestLegacyStringErrorIsSanitized', () async {
    await attachWith({
      'ok': false,
      'error': 'HTTP 403 POST http://127.0.0.1:18090/v1/relay/ticket body={"code":300006}',
    });

    final f = SessionVideo.instance.failure;
    expect(f, isNotNull);
    // legacy 字符串路径也必须脱敏
    expect(f!.display.contains('http://'), isFalse);
    expect(f.display.contains('body='), isFalse);
  });

  test('TestAttachSuccessClearsPreviousFailure', () async {
    await attachWith({
      'ok': false,
      'error': {'message': 'x'},
    });
    expect(SessionVideo.instance.failure, isNotNull);

    SessionVideo.attachRunner = (args) async => {
      'ok': true,
      'session_id': 'ctrl-1',
    };
    await SessionVideo.instance.attach(
      gateway: 'g',
      relay: 'r:19000',
      token: 't',
      deviceId: 'd',
      deviceToken: 'dt',
      sessionId: 's',
    );
    expect(SessionVideo.instance.failure, isNull, reason: '成功后必须清旧失败态');
  });
}
