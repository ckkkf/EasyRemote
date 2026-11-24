// 连接请求的「看得见」与「能退出」：以前只有一个 18px 转圈，卡住就是死界面。
import 'dart:async';
import 'dart:convert';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/pages/connect_page.dart';
import 'package:easyremote_client/prefs.dart';
import 'package:easyremote_client/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Widget _page(GatewayClient client) => MaterialApp(
  theme: buildTheme(Brightness.light),
  home: Scaffold(
    body: ConnectPage(
      client: client,
      initialCode: '123456789',
      recents: const <String>[],
      onCodeUsed: (_) {},
      onSessionStarted: (_, _) {},
    ),
  ),
);

void main() {
  testWidgets(
    'TestDirectConnectRequestsNoPasswordAndRevealsFieldOnlyWhenNeeded',
    (tester) async {
      final previous = appPrefs.value;
      appPrefs.value = previous.copyWith(sameAccountDirect: true);
      addTearDown(() => appPrefs.value = previous);
      final bodies = <Map<String, dynamic>>[];
      final client = GatewayClient(
        baseUrl: 'http://test',
        client: MockClient((req) async {
          bodies.add(jsonDecode(req.body) as Map<String, dynamic>);
          return http.Response(
            '{"code":200004,"name":"CONN_CAPTCHA_EXPIRED","message":"需要验证码","retryable":false,"trace_id":"t1"}',
            403,
            headers: {'content-type': 'application/json'},
          );
        }),
      );
      await tester.pumpWidget(_page(client));
      expect(find.byKey(const Key('captcha-field')), findsNothing);
      await tester.tap(find.byKey(const Key('connect-button')));
      await tester.pumpAndSettle();
      expect(bodies.single, {'device_code': '123456789', 'direct': true});
      expect(find.byKey(const Key('captcha-field')), findsOneWidget);
    },
  );

  testWidgets('请求中显示等待条与取消，取消后回到可操作状态', (tester) async {
    // 永远不回包：模拟网关把连接接住但不响应。
    final hang = Completer<http.Response>();
    final client = GatewayClient(
      baseUrl: 'http://test',
      client: MockClient((_) => hang.future),
    );

    await tester.pumpWidget(_page(client));
    await tester.pump();

    await tester.tap(find.byKey(const Key('connect-button')));
    await tester.pump();

    expect(
      find.byKey(const Key('connect-pending')),
      findsOneWidget,
      reason: '请求中必须有显式等待条',
    );
    expect(
      find.byKey(const Key('connect-cancel')),
      findsOneWidget,
      reason: '请求中必须能取消',
    );
    expect(find.textContaining('已等待'), findsOneWidget);

    await tester.pump(const Duration(seconds: 2));
    expect(find.textContaining('已等待 2 秒'), findsOneWidget);

    await tester.tap(find.byKey(const Key('connect-cancel')));
    await tester.pump();

    expect(find.byKey(const Key('connect-pending')), findsNothing);
    expect(
      find.byKey(const Key('connect-button')),
      findsOneWidget,
      reason: '取消后必须回到可再次点击的状态',
    );

    // 让在途请求收尾，避免测试结束时留下未完成的 timer/Future。
    if (!hang.isCompleted) hang.complete(http.Response('{}', 200));
    await tester.pump();
    // 迟到的响应不许把界面拉回「等待中」，也不许弹出会话页。
    expect(find.byKey(const Key('connect-pending')), findsNothing);
  });

  test('请求超时会被打断，而不是一直挂着', () async {
    final client = GatewayClient(
      baseUrl: 'http://test',
      requestTimeout: const Duration(milliseconds: 40),
      client: MockClient((_) async {
        await Future<void>.delayed(const Duration(milliseconds: 300));
        return http.Response('{}', 200);
      }),
    );

    await expectLater(
      client.requestConnect('123456789'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'NET_TIMEOUT')),
    );
  });
}
