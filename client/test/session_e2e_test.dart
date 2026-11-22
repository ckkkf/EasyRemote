@Tags(['e2e-desktop'])
library;

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

import 'support/e2e_services.dart';

/// 真实会话端到端（Dart → FFI → Rust → 真实网关 + 真实中继）。
///
/// 服务由 [E2eServices] 自动启动；缺服务时失败，不再因 env 缺失 skip。
void main() {
  E2eServices? services;
  setUpAll(() async {
    services = await E2eServices.start();
  });
  tearDownAll(() async {
    await services?.stop();
  });

  test('完整会话：登录→建会话→握手→加密→解码', () {
    final runner = ErSessionRunner.load();
    final r = runner.run(
      gatewayUrl: services!.gatewayUrl,
      relayAddr: services!.relayAddr,
      phoneA: '13700009001',
      phoneB: '13700009002',
    );

    expect(r['ok'], isTrue, reason: r['error']?.toString());
    expect(r['handshake_verified'], isTrue);
    expect(r['keys_match'], isTrue);
    expect(r['plaintext_absent'], isTrue);
    expect(r['width'], greaterThan(0));
    expect(r['height'], greaterThan(0));
    expect(r['sealed_bytes'], greaterThan(r['encoded_bytes'] as int));
    // ER-502b：回环应走 P2P；失败时明确回落 relay（可观测）。
    expect(r['link'], anyOf('p2p', 'relay'), reason: 'link 必须上报');
    expect(r['link'], 'p2p', reason: '回环候选应打洞成功');
  });

  test('扁平 JSON 解析正确', () {
    final m = ErSessionRunner.parseFlatJson(
      '{"ok":true,"width":1920,"name":"abc","flag":false}',
    );
    expect(m['ok'], true);
    expect(m['width'], 1920);
    expect(m['name'], 'abc');
    expect(m['flag'], false);
  });
}
