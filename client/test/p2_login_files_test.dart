import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/pages/files_page.dart';
import 'package:easyremote_client/pages/login_page.dart';
import 'package:easyremote_client/theme.dart';

Widget wrap(Widget child) => MaterialApp(theme: buildTheme(Brightness.light), home: child);

/// 统一把视口设成桌面宽度：原型是 1280×800，窄屏会切成移动版式。
void useDesktopViewport(WidgetTester tester) {
  tester.view.physicalSize = const Size(1280, 900);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
}

http.Response ok(Map<String, dynamic> data) => http.Response(
      '{"code":0,"name":"OK","data":${_json(data)},"trace_id":"test"}',
      200,
      headers: {'content-type': 'application/json'},
    );

String _json(Object value) => const JsonEncoder().convert(value);

void main() {
  testWidgets('登录页是原型单表单：没有手机号/邮箱大 Tab，邮箱入口可点开弹窗', (tester) async {
    useDesktopViewport(tester);
    final client = GatewayClient(
      baseUrl: 'http://test',
      client: MockClient((req) async => ok(const {})),
    );
    await tester.pumpWidget(wrap(LoginPage(client: client)));

    // 原型 s01：手机号 + 验证码 + 登录/注册 + 协议 + 或 + 两个 V1 按钮。
    expect(find.byKey(const Key('phone-field')), findsOneWidget);
    expect(find.byKey(const Key('code-field')), findsOneWidget);
    expect(find.byKey(const Key('send-code')), findsOneWidget);
    expect(find.byKey(const Key('login-button')), findsOneWidget);
    expect(find.byKey(const Key('agree-row')), findsOneWidget);
    expect(find.text('微信登录'), findsOneWidget);
    expect(find.text('邮箱登录'), findsOneWidget);
    // 之前自己发挥的两个大 Tab 必须消失。
    expect(find.byKey(const Key('tab-phone')), findsNothing);
    expect(find.byKey(const Key('tab-email')), findsNothing);
    expect(find.byKey(const Key('email-field')), findsNothing);

    // 微信仍是 V1 占位：点了不弹任何东西。
    await tester.tap(find.byKey(const Key('wechat-provider')));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);

    // 邮箱入口可点，弹窗里是真实接口所需的两个输入。
    await tester.tap(find.byKey(const Key('email-provider')));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.byKey(const Key('email-field')), findsOneWidget);
    expect(find.byKey(const Key('email-code-field')), findsOneWidget);
    expect(find.byKey(const Key('send-email-code')), findsOneWidget);
  });

  testWidgets('邮箱弹窗真的调 /v1/auth/email/code 与 /v1/auth/email/login', (tester) async {
    useDesktopViewport(tester);
    final calls = <String>[];
    final client = GatewayClient(
      baseUrl: 'http://test',
      client: MockClient((req) async {
        calls.add(req.url.path);
        if (req.url.path == '/v1/auth/email/login') {
          return ok(const {'token': 'tok', 'account_id': 'acc-1'});
        }
        if (req.url.path == '/v1/devices/bind') {
          return ok(const {'device_id': 'dev-1', 'raw_code': '842615037', 'device_token': 'dt'});
        }
        if (req.url.path == '/v1/devices') return ok(const {'devices': []});
        if (req.url.path == '/v1/config') return ok(const {'config': {}});
        return ok(const {});
      }),
    );
    await tester.pumpWidget(wrap(LoginPage(client: client)));
    await tester.tap(find.byKey(const Key('email-provider')));
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('email-field')), 'demo@example.com');
    await tester.tap(find.byKey(const Key('send-email-code')));
    await tester.pumpAndSettle();
    expect(find.text('邮件验证码已发送（开发环境固定 654321）'), findsOneWidget);
    expect(calls, contains('/v1/auth/email/code'));

    await tester.enterText(find.byKey(const Key('email-code-field')), '654321');
    // 登录会真实写本地会话文件（dart:io），widget 测试的 FakeAsync 里 I/O 不会完成，
    // 所以点击放到 runAsync 里跑，再回主 zone 推进 UI。
    await tester.runAsync(() async {
      await tester.tap(find.byKey(const Key('email-login-button')));
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 120));
    }
    expect(calls, contains('/v1/auth/email/login'));
    expect(calls, contains('/v1/devices/bind'));
  });

  testWidgets('文件页复刻原型：队列 + 本机/远端双栏 + 真实上传到 100%', (tester) async {
    useDesktopViewport(tester);
    final calls = <String>[];
    final client = GatewayClient(
      baseUrl: 'http://test',
      client: MockClient((req) async {
        calls.add('${req.method} ${req.url.path}');
        if (req.url.path == '/v1/files' && req.method == 'GET') {
          return ok(const {
            'files': [
              {
                'id': 'f1',
                'name': '季度报告-2026Q4.pptx',
                'size': 1300000,
                'sha256': 'aabbccddeeff',
                'created_at': '2026-11-20T10:00:00Z',
              }
            ],
          });
        }
        if (req.url.path == '/v1/files' && req.method == 'POST') {
          return ok(const {
            'file': {
              'id': 'f2',
              'name': 'note.txt',
              'size': 24,
              'sha256': '112233445566',
              'created_at': '2026-11-20T10:05:00Z',
            },
          });
        }
        return ok(const {});
      }),
    );
    await tester.pumpWidget(wrap(Scaffold(body: FilesPage(client: client))));
    await tester.pumpAndSettle();

    // 顶栏 + 队列 + 双栏都在。
    expect(find.text('文件传输'), findsOneWidget);
    expect(find.text('传输队列'), findsOneWidget);
    expect(find.byKey(const Key('pause-all')), findsOneWidget);
    expect(find.byKey(const Key('clear-done')), findsOneWidget);
    expect(find.text('本机'), findsOneWidget);
    expect(find.text('远端'), findsOneWidget);
    expect(find.text('季度报告-2026Q4.pptx'), findsOneWidget);

    // 本机栏的文本入口 → 加入本机栏 → 上传 → 队列里出现真实任务。
    await tester.tap(find.byKey(const Key('text-file-entry')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('text-file-submit')));
    await tester.pumpAndSettle();
    expect(find.text('note.txt'), findsOneWidget);

    await tester.tap(find.byTooltip('上传'));
    await tester.pumpAndSettle();
    expect(calls, contains('POST /v1/files'));
    expect(find.text('已完成'), findsOneWidget);
    expect(find.text('100%'), findsOneWidget);
  });
}
