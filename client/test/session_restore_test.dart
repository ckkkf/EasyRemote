// 登录态持久化回归：刷新页面不该回登录页。
//
// 覆盖三件事：
//   1) 登录 + 绑定设备后落盘，新实例（等价于刷新后的新进程）能恢复出同一台设备；
//   2) 令牌过期（AUTH_TOKEN_EXPIRED）时清空快照并回登录页；
//   3) 断网/服务端未起时不踢人，先用本地设备身份进主界面。
import 'dart:convert';
import 'dart:io';

import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/session_store_io.dart' as store;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _trace = 'trace-test';

http.Response _ok(Map<String, dynamic> data) => http.Response(
  jsonEncode({'code': 0, 'name': 'OK', 'data': data, 'trace_id': _trace}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

http.Response _expired() => http.Response(
  jsonEncode({
    'code': 100002,
    'name': 'AUTH_TOKEN_EXPIRED',
    'message': '登录已过期，请重新登录',
    'retryable': false,
    'trace_id': _trace,
  }),
  401,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

http.Client _fake({bool tokenValid = true, bool offline = false}) {
  return MockClient((req) async {
    if (offline) throw const SocketException('connection refused');
    switch (req.url.path) {
      case '/v1/auth/login':
        return _ok({'account_id': 'acc-1', 'token': 'tok-1'});
      case '/v1/devices/bind':
        return _ok({
          'device_id': 'dev-1',
          'device_token': 'dtok-1',
          'raw_code': '123456789',
        });
      case '/v1/devices':
        if (!tokenValid) return _expired();
        // 恢复时必须真的把令牌带上，否则等于没校验。
        expect(req.headers['authorization'], 'Bearer tok-1');
        return _ok({
          'devices': [
            {'id': 'dev-1', 'raw_code': '123456789', 'platform': 'macos', 'online': true},
          ],
        });
    }
    return http.Response('{"code":404,"name":"NOT_FOUND"}', 404);
  });
}

void main() {
  late File sessionFile;

  setUp(() {
    sessionFile = File(
        '${Directory.systemTemp.path}/er-session-test-${DateTime.now().microsecondsSinceEpoch}.json');
    store.sessionFilePathOverride = sessionFile.path;
  });

  tearDown(() async {
    if (await sessionFile.exists()) await sessionFile.delete();
  });

  test('登录并绑定设备后落盘，新实例能恢复登录态', () async {
    final first = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    await first.login('13800000000', '123456');
    final device = await first.bindDevice('macos');
    expect(device.id, 'dev-1');
    expect(device.code, '123456789');

    expect(await sessionFile.exists(), isTrue, reason: '绑定设备后必须把会话写到本地');
    final raw = jsonDecode(await sessionFile.readAsString()) as Map<String, dynamic>;
    expect(raw['token'], 'tok-1');
    expect(raw['device_token'], 'dtok-1');
    expect(raw['device_id'], 'dev-1');
    expect(raw['device_code'], '123456789');
    expect(raw.containsKey('phone'), isFalse, reason: '手机号不进本地快照');

    // 等价于刷新页面：新进程、新 GatewayClient，只靠本地快照。
    final second = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    final restored = await second.restoreSession();
    expect(restored, isNotNull);
    expect(restored!.id, 'dev-1');
    expect(restored.code, '123456789');
    expect(restored.platform, 'macos');
    expect(second.token, 'tok-1');
    expect(second.deviceToken, 'dtok-1');
  });

  test('令牌过期时清空本地快照并回登录页', () async {
    final first = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    await first.login('13800000000', '123456');
    await first.bindDevice('macos');
    expect(await sessionFile.exists(), isTrue);

    final second = GatewayClient(baseUrl: 'http://gw.test', client: _fake(tokenValid: false));
    expect(await second.restoreSession(), isNull);
    expect(await sessionFile.exists(), isFalse, reason: '过期令牌必须清掉，不能每次刷新都试一遍');
    expect(second.token, isNull);
  });

  test('断网时不踢回登录页，保留本地会话', () async {
    final first = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    await first.login('13800000000', '123456');
    await first.bindDevice('macos');

    final second = GatewayClient(baseUrl: 'http://gw.test', client: _fake(offline: true));
    final restored = await second.restoreSession();
    expect(restored, isNotNull, reason: '服务端没起也要能进主界面');
    expect(restored!.id, 'dev-1');
    expect(restored.online, isFalse);
    expect(await sessionFile.exists(), isTrue);
  });

  test('没有本地快照时直接回登录页', () async {
    final client = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    expect(await client.restoreSession(), isNull);
  });

  test('退出登录清掉内存令牌与本地快照', () async {
    final client = GatewayClient(baseUrl: 'http://gw.test', client: _fake());
    await client.login('13800000000', '123456');
    await client.bindDevice('macos');
    await client.logout();
    expect(client.token, isNull);
    expect(client.deviceToken, isNull);
    expect(await sessionFile.exists(), isFalse);
  });
}
