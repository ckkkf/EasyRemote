// 导航回归：设置必须在侧边栏最下方，且换页不重建页面（旧页面不闪、输入不丢）。
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/prefs.dart';
import 'package:easyremote_client/shell.dart';
import 'package:easyremote_client/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _device = DeviceInfo(
  id: 'dev-1',
  code: '842615037',
  platform: 'macos',
  online: true,
);

GatewayClient _client() {
  final mock = MockClient((req) async {
    if (req.url.path == '/v1/devices') {
      return http.Response(
        '{"code":0,"name":"OK","data":{"devices":[{"id":"dev-2","raw_code":"111222333","platform":"windows","online":true}]},"trace_id":"t"}',
        200,
        headers: {'content-type': 'application/json'},
      );
    }
    return http.Response(
      '{"code":404,"name":"NOT_FOUND","message":"no","trace_id":"t"}',
      404,
      headers: {'content-type': 'application/json'},
    );
  });
  return GatewayClient(baseUrl: 'http://test', client: mock);
}

void main() {
  testWidgets('设置导航固定在侧边栏最下方；切页保留页面状态', (tester) async {
    appPrefs.value = const Prefs(allowControl: false);
    // setSurfaceSize 是物理像素；DPR=1 才能得到 1400 的逻辑宽度（桌面侧边栏布局）。
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(1400, 900);
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.light),
        home: HomeShell(client: _client(), device: _device),
      ),
    );
    await tester.pumpAndSettle();

    final settings = find.text('设置');
    final files = find.text('文件传输');
    expect(settings, findsOneWidget);
    expect(files, findsOneWidget);
    expect(
      tester.getTopLeft(settings).dy,
      greaterThan(tester.getBottomLeft(files).dy),
      reason: '设置必须排在文件传输下方',
    );
    // 但它是「导航组」的最后一项，不是整栏最下方：外观/本机面板永远在导航组下面。
    final appearance = find.text('外观');
    expect(appearance, findsOneWidget);
    expect(
      tester.getTopLeft(settings).dy,
      lessThan(tester.getTopLeft(appearance).dy),
      reason: '设置要留在导航组内（外观面板之上），不能掉到整栏最底部',
    );

    // 在设备页搜索框输入，切到设置再回来，文字必须还在（IndexedStack 保活）。
    final search = find.byType(TextField).first;
    await tester.enterText(search, '111');
    await tester.pump();

    await tester.tap(settings);
    await tester.pumpAndSettle();
    expect(find.text('同账号设备免密直连'), findsOneWidget);

    await tester.tap(find.text('我的设备').first);
    await tester.pumpAndSettle();
    final searchAgain = tester.widget<TextField>(find.byType(TextField).first);
    expect(searchAgain.controller?.text, '111',
        reason: '换页不能 dispose 设备页，否则输入与滚动位置都会丢');
  });

  // 回归：导航项的底色过渡目标必须是不透明色。
  // Material 的 color 过渡按 RGBA 分量线性插值，插到 Colors.transparent（透明黑）
  // 会在中途变暗——鼠标移出时就会闪一下灰黑。悬停高亮交给 InkWell 原生处理：
  // 它做的是实色的透明度动画，不经过半透明中间色，也不会和底色叠成两层。
  testWidgets('导航项底色不透明、悬停交回 InkWell，移出时不再闪', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(1400, 900);
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.light),
        home: HomeShell(client: _client(), device: _device),
      ),
    );
    await tester.pumpAndSettle();

    final settings = find.text('设置');
    final material = tester.widget<Material>(
      find.ancestor(of: settings, matching: find.byType(Material)).first,
    );
    expect(
      material.color?.a,
      1.0,
      reason: '未选中项的底色不能是透明色，否则过渡中途会变暗闪一下',
    );

    final ink = tester.widget<InkWell>(
      find.ancestor(of: settings, matching: find.byType(InkWell)).first,
    );
    expect(ink.hoverColor, isNotNull, reason: '悬停反馈要保留（原生 InkWell）');
    expect(ink.onTap, isNotNull);
  });
}
