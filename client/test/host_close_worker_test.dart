import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/host_service.dart';

/// 顶层同步函数：模拟一次同步阻塞 FFI（Isolate.run 的可发送 tear-off）。
HostCloseResult _blockingWorker(String hostId, String sessionId) {
  sleep(const Duration(milliseconds: 200));
  return const HostCloseResult(ok: true);
}

HostCloseResult _throwingWorker(String hostId, String sessionId) {
  throw StateError('dylib missing');
}

/// ER-905：生产 wrapper 必须把同步 FFI 放到 worker isolate（UI 心跳不停），
/// 且失败要能回到调用方。注入值在 spawn 前被读入局部变量（跨 isolate 可见）。
void main() {
  tearDown(() => closeWorker = null);

  test('TestHostCloseDoesNotBlockUiHeartbeat', () async {
    closeWorker = _blockingWorker;
    var beats = 0;
    final timer = Timer.periodic(
      const Duration(milliseconds: 20),
      (_) => beats++,
    );
    final result = await closeSessionInWorker('host-1', 'ses-1');
    timer.cancel();

    expect(result.ok, isTrue);
    expect(
      beats,
      greaterThan(3),
      reason: '同步 worker 在 isolate 内跑时，UI 心跳必须继续推进',
    );
  });

  test('TestHostCloseWorkerFailureSurfacesToCaller', () async {
    closeWorker = _throwingWorker;
    Object? caught;
    try {
      await closeSessionInWorker('host-1', 'ses-1');
    } catch (e) {
      caught = e;
    }
    expect(caught, isNotNull, reason: '异常必须冒泡给调用方转成失败结果');
  });
}
