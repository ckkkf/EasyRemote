// 登录过期自动跳转 + 同账号免密直连的协议回归。
import 'dart:convert';
import 'dart:io';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/session_store_io.dart' as store;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

http.Response _ok(Map<String, dynamic> data) => http.Response(
  jsonEncode({'code': 0, 'name': 'OK', 'data': data, 'trace_id': 't'}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

http.Response _expired() => http.Response(
  jsonEncode({
    'code': 100002,
    'name': 'AUTH_TOKEN_EXPIRED',
    'message': '登录已过期，请重新登录',
    'retryable': false,
    'trace_id': 't',
  }),
  401,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  late File sessionFile;

  setUp(() async {
    sessionFile = File(
      '${Directory.systemTemp.path}/er-auth-test-${DateTime.now().microsecondsSinceEpoch}.json',
    );
    store.sessionFilePathOverride = sessionFile.path;
  });

  tearDown(() async {
    store.sessionFilePathOverride = null;
    try {
      if (await sessionFile.exists()) await sessionFile.delete();
    } catch (_) {}
  });

  test('任意接口返回 AUTH_TOKEN_EXPIRED → authExpired 通知一次并清令牌', () async {
    final client = GatewayClient(
      baseUrl: 'http://gw.test',
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/login') {
          return _ok({'token': 'tok-1'});
        }
        if (req.url.path == '/v1/devices') return _expired();
        return _ok(<String, dynamic>{});
      }),
    );
    await client.login('13800000000', '123456');
    expect(client.token, 'tok-1');

    await expectLater(client.listDevices(), throwsA(isA<ApiException>()));
    expect(client.authExpired.value, 1);
    expect(client.token, isNull, reason: '过期令牌必须被清掉');

    // 同一令牌并发/再次失败不应重复通知（否则登录页会叠很多层）。
    await expectLater(client.listDevices(), throwsA(isA<ApiException>()));
    expect(client.authExpired.value, 1);
  });

  test('同账号免密直连：direct=true 进入请求体', () async {
    Map<String, dynamic>? sent;
    final client = GatewayClient(
      baseUrl: 'http://gw.test',
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/login') {
          return _ok({'token': 'tok-1'});
        }
        if (req.url.path == '/v1/devices/bind') {
          return _ok({
            'device_id': 'dev-1',
            'device_token': 'dtok-1',
            'raw_code': '123456789',
          });
        }
        if (req.url.path == '/v1/connect/request') {
          sent = jsonDecode(req.body) as Map<String, dynamic>;
          return _ok({'session_id': 'sess-1', 'state': 'requested'});
        }
        return _ok(<String, dynamic>{});
      }),
    );
    await client.login('13800000000', '123456');
    await client.bindDevice('macos');
    final sid = await client.requestConnect('987654321', direct: true);
    expect(sid, 'sess-1');
    expect(sent?['direct'], isTrue);
    expect(sent?['captcha'], isNull);
  });

  test('反向代理回 "bad gateway" 纯文本：转成人话 ApiException，不抛 SyntaxError', () async {
    final client = GatewayClient(
      baseUrl: 'http://gw.test',
      client: MockClient(
        (req) async => http.Response(
          'bad gateway',
          502,
          headers: {'content-type': 'text/plain'},
        ),
      ),
    );
    await expectLater(
      client.listDevices(),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'SRV_INTERNAL')
            .having((e) => e.message, 'message', contains('网关暂时不可用'))
            .having((e) => e.message, 'message', contains('bad gateway')),
      ),
    );
  });

  test('反向代理回 HTML 错误页：提示 HTML 而不是甩一堆标签', () async {
    final client = GatewayClient(
      baseUrl: 'http://gw.test',
      client: MockClient(
        (req) async => http.Response(
          '<html><body><h1>502 Bad Gateway</h1></body></html>',
          502,
          headers: {'content-type': 'text/html'},
        ),
      ),
    );
    await expectLater(
      client.listDevices(),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'SRV_INTERNAL')
            .having((e) => e.message, 'message', contains('HTML 页面')),
      ),
    );
  });

  test('device_id 缺失时 ensureDevice 会自动补绑（修 missing session）', () async {
    var binds = 0;
    final client = GatewayClient(
      baseUrl: 'http://gw.test',
      client: MockClient((req) async {
        if (req.url.path == '/v1/auth/login') {
          return _ok({'token': 'tok-1'});
        }
        if (req.url.path == '/v1/devices/bind') {
          binds++;
          return _ok({
            'device_id': 'dev-new',
            'device_token': 'dtok-new',
            'raw_code': '111222333',
          });
        }
        return _ok(<String, dynamic>{});
      }),
    );
    await client.login('13800000000', '123456');
    // 只登录、没绑设备：历史快照就是这种形态。
    final device = await client.ensureDevice(platform: 'macos');
    expect(binds, 1);
    expect(device.id, 'dev-new');
    expect(client.deviceId, 'dev-new');
    expect(client.deviceToken, 'dtok-new');

    // 已有完整身份时不重复绑。
    final again = await client.ensureDevice(platform: 'macos');
    expect(again.id, 'dev-new');
    expect(binds, 1);
  });
}
