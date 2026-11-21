// 连接页设备码输入回归：9 个真输入框，光标/选区/退格/粘贴都必须是原生行为。
//
// 旧实现是「透明 TextField 盖在画出来的格子上」，光标被藏掉、数字选不中，
// 用户体感就是「没有光标闪、没法选中」。
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/pages/connect_page.dart';
import 'package:easyremote_client/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

GatewayClient _client() {
  final mock = MockClient(
    (req) async => http.Response(
      '{"code":404,"name":"NOT_FOUND","message":"no","trace_id":"t"}',
      404,
      headers: {'content-type': 'application/json'},
    ),
  );
  return GatewayClient(baseUrl: 'http://test', client: mock);
}

Widget _page({String initialCode = ''}) => MaterialApp(
  theme: buildTheme(Brightness.light),
  home: Scaffold(
    body: ConnectPage(
      client: _client(),
      initialCode: initialCode,
      recents: const <String>[],
      onCodeUsed: (_) {},
      onSessionStarted: (_, _) {},
    ),
  ),
);

String _digit(WidgetTester tester, int i) =>
    tester.widget<TextField>(find.byKey(Key('device-code-digit-$i'))).controller!.text;

void main() {
  testWidgets('每格都是真 TextField，且光标可见', (tester) async {
    await tester.pumpWidget(_page());
    for (var i = 0; i < 9; i++) {
      expect(find.byKey(Key('device-code-digit-$i')), findsOneWidget);
    }
    final first = tester.widget<TextField>(
      find.byKey(const Key('device-code-digit-0')),
    );
    expect(first.showCursor, isNot(false), reason: '必须显示系统光标，不能靠画假格子');
    expect(first.cursorColor, isNot(Colors.transparent));
    expect(first.enableInteractiveSelection, isNot(false), reason: '数字必须能选中');
  });

  // 回归：Material 3 的 InputDecorator 会在悬停时自己画一块灰色高亮，
  // 而格子的底色/边框是外层容器画的——两层叠起来就是鼠标划过时多出的灰色小方块。
  testWidgets('鼠标划过格子不再出现 Material 的灰色悬停块', (tester) async {
    await tester.pumpWidget(_page());
    final field = tester.widget<TextField>(
      find.byKey(const Key('device-code-digit-0')),
    );
    expect(
      field.decoration?.hoverColor,
      Colors.transparent,
      reason: '格子底色只由外层容器负责',
    );
  });

  testWidgets('逐格输入自动前移，输满 9 位后按钮可用', (tester) async {
    await tester.pumpWidget(_page());
    for (var i = 0; i < 9; i++) {
      await tester.enterText(find.byKey(Key('device-code-digit-$i')), '${i + 1}');
      await tester.pump();
    }
    expect(_digit(tester, 0), '1');
    expect(_digit(tester, 8), '9');

    final button = tester.widget<FilledButton>(
      find.byKey(const Key('connect-button')),
    );
    expect(button.onPressed, isNotNull, reason: '9 位齐了才能请求连接');
  });

  testWidgets('粘贴 9 位会自动铺满所有格子', (tester) async {
    await tester.pumpWidget(_page());
    await tester.enterText(
      find.byKey(const Key('device-code-digit-0')),
      '361285400',
    );
    await tester.pump();
    expect(
      [for (var i = 0; i < 9; i++) _digit(tester, i)].join(),
      '361285400',
    );
  });

  testWidgets('空格子上退格会回到上一格并删掉它', (tester) async {
    await tester.pumpWidget(_page(initialCode: '123'));
    await tester.pump();
    expect(_digit(tester, 2), '3');

    // 焦点放到第 4 格（空），按退格应清掉第 3 格的 3。
    await tester.tap(find.byKey(const Key('device-code-digit-3')));
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.backspace);
    await tester.pump();

    expect(_digit(tester, 2), '', reason: '空格退格必须删掉上一格');
    expect(_digit(tester, 1), '2');
  });

  testWidgets('外部带入设备码会铺进格子', (tester) async {
    await tester.pumpWidget(_page(initialCode: '919 116 436'));
    await tester.pump();
    expect(
      [for (var i = 0; i < 9; i++) _digit(tester, i)].join(),
      '919116436',
      reason: '带空格的设备码也要能落进格子',
    );
  });
}
