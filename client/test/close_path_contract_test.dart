import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/contract.dart';

/// ER-905 回归守护：`closeSession` 必须打到 GW-25 的**契约路径**。
/// 曾经手写成 /v1/connect/close（服务端无此路由 → 404），
/// 导致主控「断开连接」永远失败、会话留在服务端。
void main() {
  test('TestCloseSessionUsesContractGw25Path', () async {
    final gw25 = endpoints.firstWhere((e) => e.id == 'GW-25');
    expect(gw25.path, '/v1/sessions/close');

    final paths = <String>[];
    final client = GatewayClient(
      baseUrl: 'http://127.0.0.1:18090',
      client: MockClient((req) async {
        paths.add(req.method + ' ' + req.url.path);
        return http.Response(
          '{"code":0,"name":"OK","data":{"session_id":"ses-1","state":"closed","reason":"controller_closed"},"trace_id":"t"}',
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    await client.closeSession('ses-1');

    expect(
      paths,
      contains('POST ' + gw25.path),
      reason: 'closeSession 必须调用契约里的 GW-25 路径，而不是自造路径',
    );
    expect(
      paths.any((p) => p.contains('/v1/connect/close')),
      isFalse,
      reason: '/v1/connect/close 不存在，写它就是 404',
    );
  });
}
