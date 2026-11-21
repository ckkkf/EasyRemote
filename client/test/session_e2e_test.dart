import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

/// 真实会话端到端（Dart → FFI → Rust → 真实网关 + 真实中继）。
/// 需要 ER_GATEWAY_URL / ER_RELAY_ADDR 环境变量指向运行中的服务，
/// 否则跳过（不算失败）。
void main() {
  final gw = Platform.environment['ER_GATEWAY_URL'];
  final relay = Platform.environment['ER_RELAY_ADDR'];
  final enabled = gw != null && relay != null;

  test('完整会话：登录→建会话→握手→加密→解码', () {
    if (!enabled) {
      // ignore: avoid_print
      print('skip: ER_GATEWAY_URL / ER_RELAY_ADDR not set');
      return;
    }
    final runner = ErSessionRunner.load();
    final r = runner.run(
      gatewayUrl: gw,
      relayAddr: relay,
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
