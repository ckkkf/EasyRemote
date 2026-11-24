import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/local_control_bar.dart';
import 'package:easyremote_client/host_close_policy.dart';
import 'package:easyremote_client/host_service.dart';

/// ER-905 现象 3：被控端控制条必须能显示 pending 与失败原因，
/// 不能静默吞掉重复点击，也不能在失败后消失。
void main() {
  testWidgets('TestLocalControlBarShowsBusyAndDisablesButton', (tester) async {
    var taps = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LocalControlBar(
            sessionId: 'ses-1234567890ab',
            onDisconnect: () => taps++,
            busy: true,
          ),
        ),
      ),
    );

    expect(find.text('断开中…'), findsOneWidget);
    final btn = tester.widget<FilledButton>(
      find.byKey(const Key('local-control-disconnect')),
    );
    expect(btn.onPressed, isNull, reason: 'pending 时必须禁用，避免重复点击');
  });

  testWidgets('TestLocalControlBarShowsCloseError', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: LocalControlBar(
            sessionId: 'ses-1234567890ab',
            onDisconnect: () {},
            error: 'relay down',
          ),
        ),
      ),
    );

    expect(find.byKey(const Key('local-control-error')), findsOneWidget);
    expect(find.textContaining('relay down'), findsOneWidget);
  });

  test('TestClosedTombstonesAreBounded', () {
    final p = HostClosePolicy();
    for (var i = 0; i < HostClosePolicy.maxTombstones + 10; i++) {
      p.apply('ses-' + i.toString(), const HostCloseResult(ok: true));
    }
    // 最早的已被淘汰：不再被当作墓碑（可重新显示），但集合规模有界
    expect(p.visibleSession('ses-0'), 'ses-0');
    expect(
      p.visibleSession('ses-' + (HostClosePolicy.maxTombstones + 9).toString()),
      '',
    );
  });
}
