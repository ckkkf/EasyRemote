import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/contract.dart';
import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/pages/session_page.dart';
import 'package:easyremote_client/session_video.dart';
import 'package:easyremote_client/video_adapter.dart';

/// 可控 fake 视频适配器：不加载真实 FFI。
class FakeVideo implements VideoAdapter {
  FakeVideo({this.attachResult});

  final Map<String, dynamic>? attachResult;
  int attachCalls = 0;
  int closeCalls = 0;
  Completer<void>? gate;
  AttachFailure? _failure;
  String _error = '';
  bool _attached = false;

  @override
  ui.Image? get image => null;
  @override
  AttachFailure? get failure => _failure;
  @override
  String get error => _error;
  @override
  bool get attached => _attached;

  @override
  Future<void> attach({
    required String gateway,
    required String relay,
    required String token,
    required String deviceId,
    required String deviceToken,
    required String sessionId,
  }) async {
    attachCalls++;
    final g = gate;
    if (g != null) await g.future;
    final r = attachResult;
    if (r != null && r['ok'] != true) {
      final raw = r['error'];
      if (raw is Map) {
        _failure = AttachFailure(
          code: raw['code']?.toString() ?? '',
          name: raw['name']?.toString() ?? '',
          message: raw['message']?.toString() ?? '画面接管失败',
          retryable: raw['retryable'] == true,
          traceId: raw['trace_id']?.toString() ?? '',
        );
      } else {
        _failure = const AttachFailure(message: '画面接管失败，请重试');
      }
      _error = _failure!.display;
      return;
    }
    _failure = null;
    _error = '';
    _attached = true;
  }

  @override
  Future<void> poll() async {}

  @override
  void close() {
    closeCalls++;
    _attached = false;
    _failure = null;
    _error = '';
  }
}

GatewayClient fakeClient() => GatewayClient(
  baseUrl: 'http://127.0.0.1:18090',
  client: MockClient((req) async {
    if (req.url.path.contains('close')) {
      return http.Response(
        '{"code":0,"name":"OK","data":{"session_id":"s","state":"closed"},"trace_id":"t"}',
        200,
        headers: {'content-type': 'application/json'},
      );
    }
    return http.Response(
      '{"code":0,"name":"OK","data":{"session_id":"s","state":"active"},"trace_id":"t"}',
      200,
      headers: {'content-type': 'application/json'},
    );
  }),
);

/// 已登录的 client：SessionPage 的 _startVideo 需要 token/deviceId 才会 attach。
GatewayClient loggedInClient() {
  final c = fakeClient();
  c.debugAdoptSession(
    token: 't',
    deviceId: 'dev-1',
    deviceToken: 'dt',
    deviceCode: '123',
  );
  c.debugAdoptRuntime(
    const RuntimeEndpoints(
      gatewayURL: 'http://127.0.0.1:18090',
      signalURL: '',
      relayAddr: '127.0.0.1:19000',
      stunURL: '',
      webtransportURL: '',
      tlsRequired: false,
    ),
  );
  return c;
}

Widget wrap(VideoAdapter video) => MaterialApp(
  home: SessionPage(
    client: loggedInClient(),
    sessionId: 'ses-1',
    onLeave: () {},
    probeNat: false,
    pollSessionStatus: false,
    enableVideo: true,
    initialStatus: const SessionStatus(SessionState.active),
    video: video,
  ),
);

const failureResult = {
  'ok': false,
  'error': {
    'code': '300006',
    'name': 'SESSION_AUTH_WINDOW_EXCEEDED',
    'message': '本次授权已到期，请重新连接',
    'retryable': false,
    'trace_id': 'tr-ui-copy',
  },
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('TestSessionPageShowsSanitizedAttachError', (tester) async {
    final video = FakeVideo(attachResult: failureResult);
    await tester.pumpWidget(wrap(video));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('attach-error-message')), findsOneWidget);
    final text = tester
        .widget<Text>(find.byKey(const Key('attach-error-message')))
        .data!;
    expect(text.contains('本次授权已到期'), isTrue);
    expect(text.contains('http://'), isFalse);
    expect(text.contains('POST'), isFalse);
    expect(text.contains('body='), isFalse);
  });

  testWidgets('TestSessionPageCopiesTraceIdFromButton', (tester) async {
    String? copied;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            copied = (call.arguments as Map)['text'] as String?;
          }
          return null;
        });

    final video = FakeVideo(attachResult: failureResult);
    await tester.pumpWidget(wrap(video));
    await tester.pumpAndSettle();

    final btn = find.byKey(const Key('attach-error-copy-trace'));
    expect(btn, findsOneWidget, reason: 'trace_id 必须可见');
    await tester.tap(btn);
    await tester.pump();
    expect(copied, 'tr-ui-copy');
  });

  testWidgets('TestReconnectButtonAttachesAgain', (tester) async {
    final video = FakeVideo(attachResult: {'ok': true, 'session_id': 'ctrl-1'});
    await tester.pumpWidget(wrap(video));
    await tester.pumpAndSettle();
    final first = video.attachCalls;

    await tester.tap(find.byKey(const Key('reconnect')));
    await tester.pumpAndSettle();
    expect(video.attachCalls, first + 1, reason: '重连必须真的再次 attach');
  });

  testWidgets('TestRapidReconnectDoesNotAttachConcurrently', (tester) async {
    final video = FakeVideo(attachResult: {'ok': true, 'session_id': 'ctrl-1'});
    await tester.pumpWidget(wrap(video));
    await tester.pumpAndSettle();
    final base = video.attachCalls;

    video.gate = Completer<void>();
    await tester.tap(find.byKey(const Key('reconnect')));
    await tester.tap(find.byKey(const Key('reconnect')));
    await tester.tap(find.byKey(const Key('reconnect')));
    await tester.pump();
    expect(video.attachCalls, base + 1, reason: '并发点击只能一次 attach in flight');

    video.gate!.complete();
    await tester.pumpAndSettle();
    expect(video.attachCalls, base + 1);
  });
}
