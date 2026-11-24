import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/contract.dart';
import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/pages/session_page.dart';

/// ER-905：所有退出入口（返回 / 取消 / live 顶栏返回 / 系统返回）
/// 都必须走 close-before-leave。任何入口绕过 coordinator，下面的断言会红。
void main() {
  late List<String> calls;
  late int leaveCalls;

  /// GW-25 的真实路径**只来自生成契约**（contract.dart），不在这里手写。
  /// 之前手写成 /v1/connect/close，服务端根本没有该路由 → 404 → 断开永远失败。
  final closePath = endpoints.firstWhere((e) => e.id == 'GW-25').path;

  GatewayClient fakeClient() {
    final mock = MockClient((req) async {
      calls.add(req.method + ' ' + req.url.path);
      if (req.url.path == closePath) {
        return http.Response(
          '{"code":0,"name":"OK","data":{"session_id":"ses-1","state":"closed","reason":"controller_closed"},"trace_id":"t1"}',
          200,
          headers: {'content-type': 'application/json'},
        );
      }
      return http.Response(
        '{"code":0,"name":"OK","data":{"session_id":"ses-1","state":"requested"},"trace_id":"t2"}',
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    return GatewayClient(baseUrl: 'http://127.0.0.1:18090', client: mock);
  }

  // 注入式构造：不启 NAT 探测定时器、不轮询、不加载 FFI，状态直接给定。
  // 断言只验证「入口 -> GW-25 -> onLeave」的顺序，不靠 pump 固定等待。
  Widget wrap(GatewayClient c, {SessionStatus? initialStatus}) => MaterialApp(
    home: SessionPage(
      client: c,
      sessionId: 'ses-1',
      onLeave: () => leaveCalls++,
      probeNat: false,
      pollSessionStatus: false,
      enableVideo: false,
      initialStatus: initialStatus,
    ),
  );

  bool closedCalled() => calls.any((c) => c == 'POST ' + closePath);

  setUp(() {
    calls = <String>[];
    leaveCalls = 0;
  });
  testWidgets('TestConfirmedClosePopsRealNavigatorRouteOnlyOnce', (
    tester,
  ) async {
    final client = fakeClient();
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => SessionPage(
                    client: client,
                    sessionId: 'ses-1',
                    onLeave: () {
                      leaveCalls++;
                      // Match HomeShell: checks PopScope before removing the route.
                      if (leaveCalls == 1) Navigator.of(context).maybePop();
                    },
                    probeNat: false,
                    pollSessionStatus: false,
                    enableVideo: false,
                  ),
                ),
              ),
              child: const Text('设备页'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('设备页'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('cancel-connect')));
    await tester.pumpAndSettle();
    expect(find.text('设备页'), findsOneWidget);
    expect(find.byType(SessionPage), findsNothing);
    expect(calls.where((call) => call == 'POST ' + closePath), hasLength(1));
    expect(leaveCalls, 1);
    expect(find.text('已断开连接'), findsOneWidget);
  });

  testWidgets('TestBackToDevicesClosesBeforeLeaving', (tester) async {
    await tester.pumpWidget(wrap(fakeClient()));
    await tester.pump();
    await tester.tap(find.byKey(const Key('back-to-devices')));
    await tester.pumpAndSettle();

    expect(closedCalled(), isTrue, reason: '返回按钮必须先调 GW-25');
    expect(leaveCalls, 1);
  });

  testWidgets('TestCancelConnectClosesBeforeLeaving', (tester) async {
    await tester.pumpWidget(wrap(fakeClient()));
    await tester.pump();
    await tester.tap(find.byKey(const Key('cancel-connect')));
    await tester.pumpAndSettle();

    expect(closedCalled(), isTrue);
    expect(leaveCalls, 1);
  });

  testWidgets('TestLiveBackButtonClosesBeforeLeaving', (tester) async {
    await tester.pumpWidget(
      wrap(
        fakeClient(),
        initialStatus: const SessionStatus(SessionState.active),
      ),
    );
    await tester.pump();
    expect(find.byKey(const Key('back-to-devices-live')), findsOneWidget);

    await tester.tap(find.byKey(const Key('back-to-devices-live')));
    await tester.pumpAndSettle();

    expect(closedCalled(), isTrue, reason: 'live 返回也必须先关服务端会话');
    expect(leaveCalls, 1);
  });

  testWidgets('TestSystemBackClosesBeforeLeaving', (tester) async {
    await tester.pumpWidget(wrap(fakeClient()));
    await tester.pump();

    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    expect(closedCalled(), isTrue, reason: '系统返回不得绕过 GW-25');
    expect(leaveCalls, 1);
  });
}
