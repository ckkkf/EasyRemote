import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/host_close_policy.dart';
import 'package:easyremote_client/host_service.dart';

/// ER-905 现象 3：被控端断开必须由 **closeSession 结果**驱动。
/// 测的是生产实现 HostClosePolicy（不是测试内 fake），断言三条不变量：
/// 1. 失败不清控制条；2. 成功才记已关闭；3. 已关闭会话不被轮询复活。
void main() {
  test('TestLocalDisconnectFailureKeepsControlBar', () {
    final p = HostClosePolicy();
    final out = p.apply(
      'ses-1',
      const HostCloseResult(ok: false, error: 'relay down'),
    );
    expect(out.cleared, isFalse, reason: '失败不得清控制条');
    expect(out.error, 'relay down');
    expect(p.visibleSession('ses-1'), 'ses-1');
  });

  test('TestLocalControlBarClearsAfterConfirmedClose', () {
    final p = HostClosePolicy();
    final out = p.apply('ses-1', const HostCloseResult(ok: true));
    expect(out.cleared, isTrue);
    expect(p.visibleSession('ses-1'), '', reason: '已关闭会话不得被轮询写回');
  });

  test('TestCloseFailureWithoutReasonStillReports', () {
    final p = HostClosePolicy();
    final out = p.apply('ses-2', const HostCloseResult(ok: false));
    expect(out.cleared, isFalse);
    expect(out.error, isNotEmpty);
  });
}
