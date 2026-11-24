import 'package:flutter_test/flutter_test.dart';
import 'package:easyremote_client/models.dart';

/// ER-905 现象 4：主控必须能从服务端 reason 收敛成明确用户语义。
/// 之前 `closed` 没有映射（落到 idle），reason 也只是塞进 message 字符串。
void main() {
  test('TestClosedStateReasonMapping', () {
    expect(
      SessionCloseReason.fromServer('user_rejected'),
      SessionCloseReason.userRejected,
    );
    expect(
      SessionCloseReason.fromServer('consent_timeout'),
      SessionCloseReason.consentTimeout,
    );
    expect(
      SessionCloseReason.fromServer('peer_closed'),
      SessionCloseReason.peerClosed,
    );
    expect(
      SessionCloseReason.fromServer('controller_closed'),
      SessionCloseReason.closedBySelf,
    );
    // 被控端本地断开的真实 reason（跨进程 E2E 路径 B 的观测值）：
    // 对主控来说就是「对方已经结束控制」，不能落 unknown。
    expect(
      SessionCloseReason.fromServer('local_disconnect'),
      SessionCloseReason.peerClosed,
    );
    expect(
      SessionCloseReason.fromServer('local_disconnect').userMessage,
      '对方已结束远程控制',
    );
    expect(SessionCloseReason.fromServer(''), SessionCloseReason.unknown);

    // 文案必须能区分四种收敛结果，不能都显示「已结束」
    expect(SessionCloseReason.userRejected.userMessage, '对方拒绝了本次连接');
    expect(SessionCloseReason.consentTimeout.userMessage, '对方未响应，连接已取消');
    expect(SessionCloseReason.peerClosed.userMessage, '对方已结束远程控制');
  });

  test('TestSessionStatusCarriesReasonAndEndedIsTerminal', () {
    const s = SessionStatus(SessionState.ended, reason: 'user_rejected');
    expect(s.isLive, isFalse, reason: 'closed 不得被当作 live/等待');
    expect(s.reason, 'user_rejected');
  });
}
