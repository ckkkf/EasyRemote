import 'host_service.dart';

/// 被控端断开的结果策略（ER-905）。
///
/// 抽成独立类的原因：UI 只是渲染，**判据**必须有唯一来源并且可单测。
/// 关键不变量：
/// 1. `closeSession` 失败时不得清控制条（服务端会话还在）；
/// 2. 只有确认成功才把 session 记为已关闭；
/// 3. 已关闭的 session 即使被轮询返回，也必须视为 idle（否则控制条“消失又出现”）。
class HostClosePolicy {
  /// 已关闭会话的墓碑集合。
  ///
  /// 有界：只保留最近 [maxTombstones] 条，避免长时间运行时无界增长；
  /// 它只是**轮询去重**，不能替代 Rust 侧的真正收敛（见 host_stream 的 1s 状态兜底）。
  final List<String> _closedOrder = <String>[];
  static const int maxTombstones = 32;

  Set<String> get _closed => _closedOrder.toSet();

  /// 记录一次断开结果，返回 UI 应如何处理。
  HostCloseOutcome apply(String sessionId, HostCloseResult result) {
    if (result.ok) {
      if (sessionId.isNotEmpty) {
        _closedOrder.remove(sessionId);
        _closedOrder.add(sessionId);
        while (_closedOrder.length > maxTombstones) {
          _closedOrder.removeAt(0);
        }
      }
      return const HostCloseOutcome(cleared: true, error: '');
    }
    return HostCloseOutcome(
      cleared: false,
      error: result.error.isEmpty ? '断开失败' : result.error,
    );
  }

  /// 轮询兜底：已确认关闭的会话不再出现在控制条上。
  String visibleSession(String sessionId) =>
      _closed.contains(sessionId) ? '' : sessionId;

  /// 被控服务重启/换会话时清空历史。
  void reset() => _closedOrder.clear();
}

/// UI 侧处理结论。
class HostCloseOutcome {
  const HostCloseOutcome({required this.cleared, required this.error});

  /// true 表示可以清空本地会话状态。
  final bool cleared;

  /// 失败原因（cleared=false 时非空）。
  final String error;
}
