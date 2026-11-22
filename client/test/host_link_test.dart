@Tags(['e2e-desktop'])
library;

import 'dart:isolate';

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

import 'support/e2e_services.dart';

/// 顶层的 serve 任务：FFI 阻塞调用必须放在后台 isolate。
Map<String, dynamic> _serveTask(Map<String, String> args) {
  return ErHostLink.load().serve(
    hostId: args['hostId']!,
    signalBase: args['signalBase']!,
    frames: 5,
    timeoutSecs: 30,
  );
}

/// Isolate 入口：只接收可发送的原始值，避免把 Process 等不可发送对象带进 isolate。
void _serveEntry(Map<String, Object> args) {
  final port = args['port'] as SendPort;
  port.send(
    _serveTask({
      'hostId': args['hostId'] as String,
      'signalBase': args['signalBase'] as String,
    }),
  );
}

/// 真实两段式会话：被控端经信令发现会话，主控用设备码+验证码连接。
///
/// 服务由 [E2eServices] 自动启动（随机端口）；也可以用 ER_GATEWAY_URL /
/// ER_RELAY_ADDR 指向外部服务。缺服务时测试失败，不再 skip。
void main() {
  E2eServices? services;
  setUpAll(() async {
    services = await E2eServices.start();
  });
  tearDownAll(() async {
    await services?.stop();
  });

  test('设备码 + 验证码 的完整会话', () async {
    final gw = services!.gatewayUrl;
    final relay = services!.relayAddr;
    final link = ErHostLink.load();

    final prepared = link.prepare(
      gatewayUrl: gw,
      relayAddr: relay,
      phone: '13700040001',
    );
    expect(prepared['ok'], isTrue, reason: prepared['error']?.toString());
    final hostId = prepared['host_id'] as String;
    final deviceCode = prepared['device_code'] as String;
    final captcha = prepared['captcha'] as String;
    expect(deviceCode.length, 9);
    expect(captcha.length, greaterThan(0));

    // 被控端在后台 isolate 等信令。用 Isolate.spawn + SendPort 而不是 Isolate.run：
    // 闭包会把同作用域的 E2eServices（含 Process）一起捕获，导致 isolate 消息不可发送。
    final ready = ReceivePort();
    await Isolate.spawn(_serveEntry, <String, Object>{
      'hostId': hostId,
      'signalBase': gw,
      'port': ready.sendPort,
    });
    final serving = ready.first.then((msg) {
      ready.close();
      return Map<String, dynamic>.from(msg as Map);
    });

    // 等被控端真实上线（信令连接建立）后再发起一次性连接：
    // 验证码是一次性的，盲目重试只会烧掉 captcha 并触发账号/短信限流。
    // 若 serve 在建立信令前就失败，立刻把真实错误抛出来，而不是干等超时。
    await Future.any<void>([
      services!.waitForPresence(1),
      serving.then((r) {
        throw StateError('serve exited before presence: ${r['error']}');
      }),
    ]);
    final connecting = Future<Map<String, dynamic>>.delayed(
      Duration.zero,
      () => link.connect(
        gatewayUrl: gw,
        relayAddr: relay,
        phone: '13700040002',
        deviceCode: deviceCode,
        captcha: captcha,
        frames: 5,
      ),
    );
    final connected = await Future.any<Map<String, dynamic>>([
      connecting,
      serving.then((r) {
        if (r['ok'] != true) {
          throw StateError('host serve failed: ${r['error']}');
        }
        return r;
      }),
    ]);
    expect(
      connected['ok'],
      isTrue,
      reason: '${connected['error']}\n--- services log ---\n${services!.log}',
    );

    final served = await serving;
    expect(served['ok'], isTrue, reason: served['error']?.toString());
    // UDP 中继不保证「发 5 收 5」：这里的契约是数据面真的通、尺寸有效。
    // 精确帧数属于性能/弱网卡（ER-901/ER-504），不作为本 smoke 的断言。
    expect(served['frames'], greaterThan(0));
    expect(served['frames'], lessThanOrEqualTo(5));
    expect(served['width'], greaterThan(0));
    expect(served['height'], greaterThan(0));
  }, timeout: const Timeout(Duration(minutes: 2)));

  test('错误验证码被拒绝', () {
    final link = ErHostLink.load();
    final prepared = link.prepare(
      gatewayUrl: services!.gatewayUrl,
      relayAddr: services!.relayAddr,
      phone: '13700040011',
    );
    final hostId = prepared['host_id'] as String;
    try {
      final res = link.connect(
        gatewayUrl: services!.gatewayUrl,
        relayAddr: services!.relayAddr,
        phone: '13700040012',
        deviceCode: prepared['device_code'] as String,
        captcha: '000000',
        frames: 1,
      );
      expect(res['ok'], false);
    } finally {
      link.release(hostId);
    }
  });
}
