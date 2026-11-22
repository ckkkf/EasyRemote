import 'package:easyremote_client/local_control_bar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('TestLocalControlBarDisconnectsAndHasNoRemoteClose', (
    tester,
  ) async {
    var disconnected = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LocalControlBar(
            sessionId: 'sess-123456789',
            onDisconnect: () => disconnected++,
          ),
        ),
      ),
    );
    expect(find.byKey(const Key('local-control-bar')), findsOneWidget);
    expect(find.textContaining('仅本机可断开'), findsOneWidget);
    // 没有关闭/隐藏控制条的入口。
    expect(find.byTooltip('关闭'), findsNothing);
    await tester.tap(find.byKey(const Key('local-control-disconnect')));
    await tester.pump();
    expect(disconnected, 1);
  });
}
