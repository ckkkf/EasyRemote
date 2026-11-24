import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/session_video.dart';

/// ER-905：SessionVideo 的纯生命周期断言（不涉及页面渲染）。
///
/// 页面级断言在 `session_page_attach_ui_test.dart`（pump SessionPage、点 trace 复制、
/// 点重连、Completer 防并发）。本文件只覆盖 SessionVideo 自身的状态机。
int attachCalls = 0;

Future<void> failingAttach() async {
  SessionVideo.attachRunner = (args) async {
    attachCalls++;
    return {
      'ok': false,
      'error': {
        'code': '300006',
        'name': 'SESSION_AUTH_WINDOW_EXCEEDED',
        'message': '本次授权已到期，请重新连接',
        'retryable': false,
        'trace_id': 'tr-copy-me',
      },
    };
  };
  await SessionVideo.instance.attach(
    gateway: 'g',
    relay: 'r:19000',
    token: 't',
    deviceId: 'd',
    deviceToken: 'dt',
    sessionId: 's',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    attachCalls = 0;
    SessionVideo.instance.close();
  });

  tearDown(() => SessionVideo.instance.close());

  test('TestSessionVideoFailureMessageIsStable', () async {
    await failingAttach();
    final f = SessionVideo.instance.failure!;
    expect(f.display, '本次授权已到期，请重新连接');
    expect(f.display.contains('http'), isFalse);
    expect(f.traceId, 'tr-copy-me');
  });

  test('TestSessionVideoTraceIdGoesToClipboard', () async {
    String? copied;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            copied = (call.arguments as Map)['text'] as String?;
          }
          return null;
        });

    await failingAttach();
    await Clipboard.setData(
      ClipboardData(text: SessionVideo.instance.failure!.traceId),
    );
    expect(copied, 'tr-copy-me');
  });

  test('TestSessionVideoCloseClearsFailureForRetry', () async {
    await failingAttach();
    expect(SessionVideo.instance.failure, isNotNull);
    SessionVideo.instance.close();
    expect(
      SessionVideo.instance.failure,
      isNull,
      reason: 'close 必须清失败态，重试才不会看到旧错误',
    );

    await failingAttach();
    expect(attachCalls, 2, reason: '重试必须真的再次 attach');
  });
}
