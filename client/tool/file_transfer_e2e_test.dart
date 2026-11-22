@Tags(['e2e-desktop'])
library;

import 'dart:io';
import 'dart:isolate';

import 'package:flutter_test/flutter_test.dart';

import 'package:easyremote_client/native.dart';

import '../test/support/e2e_services.dart';

Map<String, dynamic> _serveTask(Map<String, String> args) {
  return ErHostLink.load().serve(
    hostId: args['hostId']!,
    signalBase: args['signalBase']!,
    frames: 100,
    timeoutSecs: 40,
  );
}

void _serveEntry(Map<String, Object> args) {
  final port = args['port'] as SendPort;
  port.send(_serveTask({
    'hostId': args['hostId'] as String,
    'signalBase': args['signalBase'] as String,
  }));
}

/// ER-711d：真实进程内文件传输（主控→被控），由外部脚本设置：
/// ER_FILE_SEND_PATH / ER_FILE_RECV_DIR。
void main() {
  final srcPath = Platform.environment['ER_FILE_SEND_PATH'];
  final recvDir = Platform.environment['ER_FILE_RECV_DIR'];
  if (srcPath == null || recvDir == null) {
    throw StateError('ER_FILE_SEND_PATH / ER_FILE_RECV_DIR must be set');
  }

  E2eServices? services;
  setUpAll(() async {
    services = await E2eServices.start();
  });
  tearDownAll(() async {
    await services?.stop();
  });

  test('TestFileTransferOverSessionDataStream', () async {
    final gw = services!.gatewayUrl;
    final relay = services!.relayAddr;
    final link = ErHostLink.load();
    final prepared = link.prepare(
      gatewayUrl: gw,
      relayAddr: relay,
      phone: '13700041001',
    );
    expect(prepared['ok'], isTrue, reason: prepared['error']?.toString());
    final hostId = prepared['host_id'] as String;
    final deviceCode = prepared['device_code'] as String;
    final captcha = prepared['captcha'] as String;

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
        phone: '13700041002',
        deviceCode: deviceCode,
        captcha: captcha,
        frames: 100,
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
    expect(connected['ok'], isTrue, reason: connected['error']?.toString());

    final src = File(srcPath);
    final expected = src.readAsBytesSync();
    final out = File('$recvDir/${src.uri.pathSegments.last}');
    final deadline = DateTime.now().add(const Duration(seconds: 20));
    while (DateTime.now().isBefore(deadline)) {
      if (out.existsSync() && out.lengthSync() == expected.length) break;
      await Future<void>.delayed(const Duration(milliseconds: 200));
    }
    expect(out.existsSync(), isTrue, reason: 'file not received in $recvDir');
    expect(out.readAsBytesSync(), equals(expected));
    // 文件到达即验收；tearDown 会停止服务。
  }, timeout: const Timeout(Duration(minutes: 2)));
}