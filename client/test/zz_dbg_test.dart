import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/pages/login_page.dart';
import 'package:easyremote_client/theme.dart';

void main() {
  testWidgets('debug send vs submit', (tester) async {
    tester.view.physicalSize = const Size(1280, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final calls = <String>[];
    final client = GatewayClient(baseUrl: 'http://test', client: MockClient((req) async {
      calls.add(req.url.path);
      return http.Response('{"code":0,"name":"OK","data":{"token":"t","device_id":"d","raw_code":"123456789","device_token":"dt"},"trace_id":"x"}', 200, headers: {'content-type': 'application/json'});
    }));
    await tester.pumpWidget(MaterialApp(theme: buildTheme(Brightness.light), home: LoginPage(client: client)));
    await tester.tap(find.byKey(const Key('email-provider')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('email-field')), 'a@b.com');
    await tester.tap(find.byKey(const Key('send-email-code')));
    for (var i = 0; i < 3; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    debugPrint('after send pumps calls=' + calls.join(','));
    debugPrint('notice=' + find.text('邮件验证码已发送（开发环境固定 654321）').evaluate().length.toString());
  });
}
