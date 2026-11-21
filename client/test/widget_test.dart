import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/pages/device_list_page.dart';
import 'package:easyremote_client/pages/diagnostics_page.dart';
import 'package:easyremote_client/pages/self_test_page.dart';
import 'package:easyremote_client/pages/settings_page.dart';
import 'package:easyremote_client/theme.dart';

const device = DeviceInfo(
  id: 'dev-1',
  code: '842615037',
  platform: 'macos',
  online: true,
);

Widget wrap(Widget child) =>
    MaterialApp(theme: buildTheme(Brightness.light), home: child);

GatewayClient fakeClient() {
  final mock = MockClient((req) async {
    if (req.url.path == '/v1/devices') {
      const body =
          '{"devices":[{"id":"dev-2","device_code":"111 222 333","platform":"windows","online":true}]}';
      return http.Response(
        body,
        200,
        headers: {'content-type': 'application/json'},
      );
    }
    return http.Response('{}', 404);
  });
  return GatewayClient(baseUrl: 'http://test', client: mock);
}

void main() {
  test('设备码格式化为 3-3-3', () {
    expect(device.prettyCode, '842 615 037');
  });

  test('非 9 位设备码原样展示', () {
    const short = DeviceInfo(
      id: 'x',
      code: '123',
      platform: 'macos',
      online: false,
    );
    expect(short.prettyCode, '123');
  });

  testWidgets('设置页展示隐私屏与防睡眠开关', (tester) async {
    await tester.pumpWidget(wrap(Scaffold(body: SettingsPage(device: device))));
    expect(find.text('隐私屏'), findsOneWidget);
    expect(find.text('会话期间阻止睡眠'), findsOneWidget);
    // 「本机」区块在运行参数下面，需要滚动到可见位置才会被构建。
    await tester.scrollUntilVisible(find.text('本机设备码'), 200);
    expect(find.text('842 615 037'), findsOneWidget);
  });

  testWidgets('隐私屏开关可切换', (tester) async {
    await tester.pumpWidget(wrap(Scaffold(body: SettingsPage(device: device))));
    final privacySwitch = find.widgetWithText(SwitchListTile, '隐私屏');
    await tester.ensureVisible(privacySwitch);
    await tester.pumpAndSettle();
    final before = tester.widget<SwitchListTile>(privacySwitch).value;
    await tester.tap(privacySwitch);
    await tester.pumpAndSettle();
    expect(tester.widget<SwitchListTile>(privacySwitch).value, !before);
  });

  testWidgets('诊断页展示关键指标', (tester) async {
    await tester.pumpWidget(
      wrap(Scaffold(body: DiagnosticsPage(client: fakeClient()))),
    );
    expect(find.text('NAT 类型'), findsOneWidget);
    expect(find.text('打洞结果'), findsOneWidget);
    expect(find.text('网关往返时延'), findsOneWidget);
    // P1-3：出口地址卡片必须存在（真实数据来自 /v1/net/reflect）
    expect(find.text('出口地址（TCP）'), findsOneWidget);
  });

  testWidgets('设备页显示本机设备码与已绑定设备', (tester) async {
    await tester.pumpWidget(
      wrap(
        Scaffold(
          body: DeviceListPage(
            client: fakeClient(),
            ownDevice: device,
            onConnect: (_) {},
          ),
        ),
      ),
    );
    await tester.pump();
    expect(find.text('842 615 037'), findsOneWidget);

    // 「允许被控（本机共享）」描述的是本机状态，必须在设备列表**上方**。
    final host = find.textContaining('允许被控');
    expect(host, findsOneWidget);
    expect(
      tester.getTopLeft(host).dy,
      lessThan(tester.getTopLeft(find.text('设备')).dy),
      reason: '允许被控要放在设备列表上方，而不是列表后面',
    );
  });

  testWidgets('自检页渲染输入与按钮', (tester) async {
    await tester.pumpWidget(wrap(const Scaffold(body: SelfTestPage())));
    expect(find.byKey(const Key('gateway-field')), findsOneWidget);
    expect(find.byKey(const Key('relay-field')), findsOneWidget);
    expect(find.byKey(const Key('prepare-host')), findsOneWidget);
    expect(find.byKey(const Key('serve-host')), findsOneWidget);
    // 未准备主机时「等待连接」不可点
    final btn = tester.widget<FilledButton>(
      find.byKey(const Key('serve-host')),
    );
    expect(btn.onPressed, isNull);
  });
}
