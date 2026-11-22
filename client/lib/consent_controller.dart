import 'dart:async';

import 'package:flutter/foundation.dart';

/// 被控端本地授权请求。
class ConsentRequest {
  const ConsentRequest({
    required this.sessionId,
    required this.controllerLabel,
    required this.requestedAt,
    required this.expiresAt,
  });

  final String sessionId;
  final String controllerLabel;
  final DateTime requestedAt;
  final DateTime expiresAt;
}

/// 本地授权决策。
class ConsentDecision {
  const ConsentDecision({
    required this.sessionId,
    required this.accepted,
    required this.reason,
  });

  final String sessionId;
  final bool accepted;

  /// accepted | rejected | timeout | superseded
  final String reason;
}

/// C18 授权 controller：唯一决定「这次远控是否被本机允许」的地方。
///
/// 为什么必须独立：host_stream 之前收到会话就直接 consent，等于任何知道设备码的人
/// 都能连；UI 也不能自己计时/自己发决策，否则超时和旧会话注入无法统一处理。
class ConsentController extends ChangeNotifier {
  ConsentController({
    DateTime Function()? clock,
    this.timeout = const Duration(seconds: 30),
  }) : _clock = clock ?? DateTime.now;

  /// 全局实例：HostService 轮询到授权请求时写入，UI 监听后展示弹窗。
  static final ConsentController instance = ConsentController();

  final DateTime Function() _clock;
  final Duration timeout;
  final StreamController<ConsentDecision> _decisions =
      StreamController<ConsentDecision>.broadcast();

  ConsentRequest? _pending;
  Timer? _ticker;
  int _remaining = 0;

  ConsentRequest? get pending => _pending;
  bool get hasPending => _pending != null;
  int get remainingSeconds => _remaining;
  Stream<ConsentDecision> get decisions => _decisions.stream;

  /// 接收一条授权请求。已有 pending 时旧请求按 superseded 拒绝，绝不静默覆盖。
  void request({required String sessionId, required String controllerLabel}) {
    if (sessionId.isEmpty) {
      throw ArgumentError('sessionId is required');
    }
    final now = _clock();
    if (_pending != null) {
      final old = _pending!;
      _pending = null;
      _decisions.add(
        ConsentDecision(
          sessionId: old.sessionId,
          accepted: false,
          reason: 'superseded',
        ),
      );
    }
    _pending = ConsentRequest(
      sessionId: sessionId,
      controllerLabel: controllerLabel,
      requestedAt: now,
      expiresAt: now.add(timeout),
    );
    _remaining = timeout.inSeconds;
    _ticker?.cancel();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) => _tick());
    notifyListeners();
  }

  /// 本机接受。
  bool accept() =>
      decide(_pending?.sessionId ?? '', accepted: true, reason: 'accepted');

  /// 本机拒绝。
  bool reject({String reason = 'rejected'}) =>
      decide(_pending?.sessionId ?? '', accepted: false, reason: reason);

  /// 只有当前 pending 的 session 可以决策；其他来源（远程/旧会话）一律拒绝。
  bool decide(
    String sessionId, {
    required bool accepted,
    required String reason,
  }) {
    final current = _pending;
    if (current == null || current.sessionId != sessionId) return false;
    _finish(
      ConsentDecision(sessionId: sessionId, accepted: accepted, reason: reason),
    );
    return true;
  }

  void _tick() {
    final current = _pending;
    if (current == null) return;
    final left = current.expiresAt.difference(_clock()).inSeconds;
    if (left <= 0) {
      _finish(
        ConsentDecision(
          sessionId: current.sessionId,
          accepted: false,
          reason: 'timeout',
        ),
      );
      return;
    }
    if (left != _remaining) {
      _remaining = left;
      notifyListeners();
    }
  }

  void _finish(ConsentDecision decision) {
    _ticker?.cancel();
    _ticker = null;
    _pending = null;
    _remaining = 0;
    _decisions.add(decision);
    notifyListeners();
  }

  @override
  void dispose() {
    _ticker?.cancel();
    _ticker = null;
    _decisions.close();
    super.dispose();
  }
}
