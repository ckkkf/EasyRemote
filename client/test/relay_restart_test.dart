import 'package:flutter_test/flutter_test.dart';

import 'support/e2e_services.dart';

// ER-903b：真实 relay 进程被 kill 后重启，编排层仍可重新注册、网关保持健康。
void main() {
  test('TestRelayProcessRestartsAndGatewayStaysHealthy', () async {
    final services = await E2eServices.start();
    try {
      expect(await services.health(), 200);
      await services.restartRelay();
      expect(await services.health(), 200, reason: '网关不应因 relay 重启而崩溃');
    } finally {
      await services.stop();
    }
  }, timeout: const Timeout(Duration(minutes: 3)));
}
