import 'api.dart';
import 'models.dart';

/// 断开流程的收敛结果。
enum CloseOutcome {
  /// 已确认服务端 closed 并释放本地资源后离开。
  left,

  /// 会话本来就是 ended（服务端已收敛），无需再关。
  alreadyEnded,

  /// 关闭失败：**不离开**，领域状态保留，可重试。
  failed,

  /// 上一次关闭仍在进行中（重复点击被忽略）。
  busy,
}

/// 结构化的关闭失败信息。
///
/// 为什么不用 `e.toString()`：`ApiException.toString()` 只返回 message，
/// 会把 `trace_id` 丢掉，而 `trace_id` 是用户/客服定位问题的唯一凭据。
class CloseError {
  const CloseError({required this.message, this.code = '', this.traceId = ''});

  final String message;
  final String code;
  final String traceId;

  /// 展示给用户的一行文本（含可复制的 trace_id）。
  String get display =>
      traceId.isEmpty ? message : '$message（trace_id $traceId）';
}

/// 断开协调器（ER-905）。
///
/// 铁律：**只有 ended 才能直接离开**。
/// `failed` 只代表「本地这次请求/状态查询/attach 失败了」，并不证明服务端会话已 closed；
/// 页面手里有 session_id，就必须先让服务端收敛，否则会在服务端遗留会话、
/// 在被控端遗留授权申请与控制条。
class SessionCloseCoordinator {
  SessionCloseCoordinator({
    required this.closeRemote,
    required this.releaseLocal,
    required this.leave,
  });

  /// 调 GW-25，返回关闭后的权威状态。
  ///
  /// 带 reason：取消 requested 会话（cancel）与主动断开（controller_closed）语义不同，
  /// 服务端据此把 reason 写进会话，被控端才能显示正确的关闭原因。
  final Future<SessionStatus> Function(String reason) closeRemote;

  /// 释放本地数据面（停 attach/解码/轮询、清 FFI 会话）。
  final void Function() releaseLocal;

  /// 真正离开页面（只在确认 closed 或本来就是 ended 时调用）。
  final void Function(String message) leave;

  bool _closing = false;
  CloseError? _error;

  bool get closing => _closing;

  /// 最近一次关闭失败的结构化原因（含 trace_id）。
  CloseError? get error => _error;

  /// 点击「断开连接」。
  ///
  /// [state] 传**当前领域状态**；失败时调用方必须保留它（不得改成 failed），
  /// 否则下一次点击会误判为终态而跳过 GW-25。
  Future<CloseOutcome> disconnect(
    SessionState state, {
    String reason = 'controller_closed',
  }) async {
    if (state == SessionState.ended) {
      // 唯一可以跳过 GW-25 的情形：服务端已经收敛到终态
      leave('会话已结束');
      return CloseOutcome.alreadyEnded;
    }
    if (_closing) {
      return CloseOutcome.busy;
    }
    _closing = true;
    _error = null;
    try {
      final closed = await closeRemote(reason);
      if (closed.state != SessionState.ended) {
        _error = CloseError(
          message: '服务端未确认关闭（state=${closed.state.name}）',
          code: 'SESSION_CLOSE_NOT_CONFIRMED',
        );
        return CloseOutcome.failed;
      }
      releaseLocal();
      leave('已断开连接');
      return CloseOutcome.left;
    } on ApiException catch (e) {
      // 保留结构化字段：message 之外还有 code/name/trace_id
      _error = CloseError(
        message: e.message,
        code: e.name ?? e.code,
        traceId: e.traceId,
      );
      return CloseOutcome.failed;
    } catch (e) {
      _error = CloseError(message: e.toString());
      return CloseOutcome.failed;
    } finally {
      _closing = false;
    }
  }
}
