@Tags(['e2e-desktop'])
library;

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

import 'support/e2e_services.dart';

/// ER-504：5% 丢包 + 抖动/乱序下，注册/握手/加密/解码仍能完成。
void main() {
  E2eServices? services;
  setUpAll(() async {
    services = await E2eServices.start();
  });
  tearDownAll(() async {
    await services?.stop();
  });

  test('弱网丢包下会话仍可建立', () {
    final runner = ErSessionRunner.load();
    final r = runner.run(
      gatewayUrl: services!.gatewayUrl,
      relayAddr: services!.lossyRelayAddr,
      phoneA: '13700009101',
      phoneB: '13700009102',
    );
    expect(r['ok'], isTrue, reason: r['error']?.toString());
    expect(r['handshake_verified'], isTrue);
    expect(r['keys_match'], isTrue);
    expect(r['link'], anyOf('p2p', 'relay'));
  });
}
