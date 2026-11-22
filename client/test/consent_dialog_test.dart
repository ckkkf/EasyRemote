import 'package:easyremote_client/consent_controller.dart';
import 'package:easyremote_client/consent_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('弹窗展示控制者与倒计时，接受/拒绝调用 controller', (tester) async {
    final c = ConsentController(clock: () => DateTime(2026));
    final got = <ConsentDecision>[];
    c.decisions.listen(got.add);
    c.request(sessionId: 's1', controllerLabel: '137****0001');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(child: ConsentDialog(controller: c)),
        ),
      ),
    );
    expect(find.byKey(const Key('consent-dialog')), findsOneWidget);
    expect(find.textContaining('137****0001'), findsOneWidget);
    expect(find.byKey(const Key('consent-countdown')), findsOneWidget);
    expect(find.textContaining('30 秒'), findsOneWidget);
    expect(find.byKey(const Key('consent-accept')), findsOneWidget);
    expect(find.byKey(const Key('consent-reject')), findsOneWidget);

    await tester.tap(find.byKey(const Key('consent-accept')));
    await tester.pump();
    expect(got.single.accepted, isTrue);
    expect(c.hasPending, isFalse);
    c.dispose();
  });

  testWidgets('拒绝按钮产生 rejected 决策', (tester) async {
    final c = ConsentController(clock: () => DateTime(2026));
    final got = <ConsentDecision>[];
    c.decisions.listen(got.add);
    c.request(sessionId: 's2', controllerLabel: 'Windows 设备');
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Center(child: ConsentDialog(controller: c)),
        ),
      ),
    );
    await tester.tap(find.byKey(const Key('consent-reject')));
    await tester.pump();
    expect(got.single.accepted, isFalse);
    expect(got.single.reason, 'rejected');
    c.dispose();
  });
}
