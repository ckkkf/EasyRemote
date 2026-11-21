import 'dart:io';
import 'dart:isolate';

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

/// 顶层的 serve 任务：FFI 阻塞调用必须放在后台 isolate。
Map<String, dynamic> _serveTask(Map<String, String> args) {
  return ErHostLink.load().serve(
        hostId: args['hostId']!,
        signalBase: args['signalBase']!,
        frames: 5,
        timeoutSecs: 30,
      );
}

/// 真实两段式会话：被控端经信令发现会话，主控用设备码+验证码连接。
void main() {
  final gw = Platform.environment['ER_GATEWAY_URL'];
  final relay = Platform.environment['ER_RELAY_ADDR'];
  final enabled = gw != null && relay != null;

  test('设备码 + 验证码 的完整会话', () async {
    if (!enabled) {
      // ignore: avoid_print
      print('skip: ER_GATEWAY_URL / ER_RELAY_ADDR not set');
      return;
    }
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

    // 被控端在后台 isolate 等信令
    final serving = Isolate.run(() => _serveTask({
          'hostId': hostId,
          'signalBase': gw,
        }));

    // 给信令连接一点时间建立
    await Future<void>.delayed(const Duration(milliseconds: 800));

    final connected = link.connect(
      gatewayUrl: gw,
      relayAddr: relay,
      phone: '13700040002',
      deviceCode: deviceCode,
      captcha: captcha,
      frames: 5,
    );
    expect(connected['ok'], isTrue, reason: connected['error']?.toString());

    final served = await serving;
    expect(served['ok'], isTrue, reason: served['error']?.toString());
    expect(served['frames'], 5);
    expect(served['width'], greaterThan(0));
    expect(served['height'], greaterThan(0));
  }, timeout: const Timeout(Duration(minutes: 2)));

  test('错误验证码被拒绝', () {
    if (!enabled) return;
    final link = ErHostLink.load();
    final prepared = link.prepare(
      gatewayUrl: gw,
      relayAddr: relay,
      phone: '13700040011',
    );
    final hostId = prepared['host_id'] as String;
    try {
      final res = link.connect(
        gatewayUrl: gw,
        relayAddr: relay,
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
