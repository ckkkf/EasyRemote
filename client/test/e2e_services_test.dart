import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'support/e2e_services.dart';

void main() {
  test('TestE2EHarnessStartsAndStopsServices', () async {
    final services = await E2eServices.start();
    expect(services.external, isFalse);
    final gatewayPort = Uri.parse(services.gatewayUrl).port;
    final relayPort = int.parse(services.relayAddr.split(':').last);

    expect(await services.health(), 200, reason: 'stack 必须能通过 /healthz');
    expect(services.log, contains('relay listening on'));

    await services.stop();
    // 停止后 health 必须不可达（0 = 连接失败/超时），证明端口真的关了。
    expect(await services.health(), 0);

    // 端口可重新绑定 = 子进程和 socket 已释放。
    final tcp = await ServerSocket.bind(
      InternetAddress.loopbackIPv4,
      gatewayPort,
    );
    await tcp.close();
    final udp = await RawDatagramSocket.bind(
      InternetAddress.loopbackIPv4,
      relayPort,
    );
    udp.close();

    // 幂等：再次 stop 不抛。
    await services.stop();
  }, timeout: const Timeout(Duration(minutes: 2)));

  test('TestE2EHarnessUsesExternalServicesWhenProvided', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((req) async {
      req.response.statusCode = 200;
      req.response.write(
        '{"code":0,"name":"OK","data":{"ok":true},"trace_id":"t"}',
      );
      await req.response.close();
    });
    try {
      final services = await E2eServices.start(
        gatewayUrl: 'http://127.0.0.1:${server.port}',
        relayAddr: '127.0.0.1:9',
      );
      expect(services.external, isTrue, reason: '显式给外部地址时不得自建进程');
      expect(await services.health(), 200);
      await services.stop();
    } finally {
      await server.close(force: true);
    }
  });
}
