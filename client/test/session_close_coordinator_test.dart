import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/api.dart';
import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/session_close_coordinator.dart';

/// ER-905 4.3：**只有 ended 可以跳过 GW-25**。
/// requested / authorized / active / failed 都必须先让服务端收敛。
void main() {
  late int closeCalls;
  late int releaseCalls;
  late List<String> leftMessages;
  late SessionStatus closeResult;
  late Object? closeError;
  late Completer<void>? gate;
  late List<String> closeReasons;

  SessionCloseCoordinator build() => SessionCloseCoordinator(
    closeRemote: (reason) async {
      closeCalls++;
      closeReasons.add(reason);
      final g = gate;
      if (g != null) await g.future;
      final err = closeError;
      if (err != null) throw err;
      return closeResult;
    },
    releaseLocal: () => releaseCalls++,
    leave: (m) => leftMessages.add(m),
  );

  setUp(() {
    closeCalls = 0;
    closeReasons = <String>[];
    releaseCalls = 0;
    leftMessages = <String>[];
    closeResult = const SessionStatus(SessionState.ended);
    closeError = null;
    gate = null;
  });

  test('TestRequestedCancelCallsCloseBeforeLeaving', () async {
    final c = build();
    expect(await c.disconnect(SessionState.waitingConsent), CloseOutcome.left);
    expect(closeCalls, 1);
    expect(releaseCalls, 1);
    expect(leftMessages.single, '已断开连接');
  });

  test('TestFailedStateStillCallsClose', () async {
    // failed 只代表本地这次失败，不证明服务端已 closed
    final c = build();
    expect(await c.disconnect(SessionState.failed), CloseOutcome.left);
    expect(closeCalls, 1, reason: 'failed 不是终态，必须调 GW-25');
    expect(releaseCalls, 1);
  });

  test('TestEndedStateSkipsCloseAndLeavesDirectly', () async {
    final c = build();
    expect(await c.disconnect(SessionState.ended), CloseOutcome.alreadyEnded);
    expect(closeCalls, 0);
    expect(releaseCalls, 0);
  });

  test('TestActiveCloseFailureKeepsStateAndAllowsRetry', () async {
    closeError = const ApiException(
      'SRV_INTERNAL',
      'relay unavailable',
      name: 'SRV_INTERNAL',
      traceId: 'trace-abc123',
    );
    final c = build();
    expect(await c.disconnect(SessionState.active), CloseOutcome.failed);
    expect(releaseCalls, 0, reason: '失败不得释放数据面');
    expect(leftMessages, isEmpty, reason: '失败不得离开页面');
    // 结构化错误必须保留 trace_id（toString 会丢）
    expect(c.error?.traceId, 'trace-abc123');
    expect(c.error?.display, contains('trace-abc123'));

    closeError = null;
    expect(await c.disconnect(SessionState.active), CloseOutcome.left);
    expect(closeCalls, 2);
    expect(releaseCalls, 1);
  });

  test('TestRepeatedClicksSendCloseOnce', () async {
    gate = Completer<void>();
    final c = build();
    final first = c.disconnect(SessionState.active);
    final second = c.disconnect(SessionState.active);
    expect(await second, CloseOutcome.busy);
    gate!.complete();
    expect(await first, CloseOutcome.left);
    expect(closeCalls, 1, reason: '重复点击只应发一次 GW-25');
  });

  test('TestCloseNotConfirmedDoesNotRelease', () async {
    closeResult = const SessionStatus(SessionState.authorized);
    final c = build();
    expect(await c.disconnect(SessionState.active), CloseOutcome.failed);
    expect(releaseCalls, 0);
    expect(leftMessages, isEmpty);
  });
}
