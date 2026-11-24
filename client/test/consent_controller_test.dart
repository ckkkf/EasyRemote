import 'package:easyremote_client/consent_controller.dart';
import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('TestConsentAccept', () {
    fakeAsync((async) {
      final now = DateTime(2026, 1, 1, 12);
      final c = ConsentController(clock: () => now);
      final got = <ConsentDecision>[];
      c.decisions.listen(got.add);
      c.request(sessionId: 's1', controllerLabel: '137****0001');
      expect(c.hasPending, isTrue);
      expect(c.remainingSeconds, 30);

      expect(c.accept(), isTrue);
      async.flushMicrotasks();
      expect(got.single.sessionId, 's1');
      expect(got.single.accepted, isTrue);
      expect(got.single.reason, 'accepted');
      expect(c.hasPending, isFalse);
      c.dispose();
    });
  });

  test('TestConsentReject', () {
    fakeAsync((async) {
      final c = ConsentController(clock: () => DateTime(2026));
      final got = <ConsentDecision>[];
      c.decisions.listen(got.add);
      c.request(sessionId: 's2', controllerLabel: 'Windows 设备');
      expect(c.reject(), isTrue);
      async.flushMicrotasks();
      expect(got.single.accepted, isFalse);
      expect(got.single.reason, 'rejected');
      c.dispose();
    });
  });

  test('TestConsentTimeout', () {
    fakeAsync((async) {
      var now = DateTime(2026, 1, 1, 12);
      final c = ConsentController(
        clock: () => now,
        timeout: const Duration(seconds: 30),
      );
      final got = <ConsentDecision>[];
      c.decisions.listen(got.add);
      c.request(sessionId: 's3', controllerLabel: 'macOS 设备');

      now = now.add(const Duration(seconds: 30));
      async.elapse(const Duration(seconds: 1));
      async.flushMicrotasks();

      expect(got.single.sessionId, 's3');
      expect(got.single.accepted, isFalse);
      expect(got.single.reason, 'timeout');
      expect(c.hasPending, isFalse, reason: '超时后必须主动清空，不能等下次交互');
      c.dispose();
    });
  });

  test('TestCancelledNativeRequestCannotBeAccepted', () {
    fakeAsync((async) {
      final controller = ConsentController();
      final decisions = <ConsentDecision>[];
      controller.decisions.listen(decisions.add);
      controller.request(sessionId: 'cancelled', controllerLabel: '测试设备');
      controller.dismiss();
      expect(controller.hasPending, isFalse);
      expect(controller.accept(), isFalse);
      async.elapse(const Duration(seconds: 31));
      expect(decisions, isEmpty);
      controller.dispose();
    });
  });

  test('TestRemoteCannotDecide', () {
    fakeAsync((async) {
      final c = ConsentController(clock: () => DateTime(2026));
      final got = <ConsentDecision>[];
      c.decisions.listen(got.add);
      c.request(sessionId: 's4', controllerLabel: '未知设备');

      expect(
        c.decide('other-session', accepted: true, reason: 'accepted'),
        isFalse,
      );
      async.flushMicrotasks();
      expect(got, isEmpty);
      expect(c.pending?.sessionId, 's4');

      c.request(sessionId: 's5', controllerLabel: '另一台设备');
      async.flushMicrotasks();
      expect(got.first.sessionId, 's4');
      expect(got.first.reason, 'superseded');
      expect(c.pending?.sessionId, 's5');
      c.dispose();
    });
  });
}
