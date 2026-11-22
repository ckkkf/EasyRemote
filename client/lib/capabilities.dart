// 能力门：把「能力成熟度台账」翻译成「现在能不能给用户用」。
//
// 为什么需要它：代码里很多入口（被控开关、隐私屏、文件页、连接按钮）已经画好了，
// 但真实闭环没打通。继续暴露入口等于把未完成说成可用。台账（docs/spec/capabilities.yaml）
// 是唯一来源，本文件只做判定，不保存任何可被 UI 改写的开关。
import 'package:flutter/foundation.dart';

import 'capability_gen.dart';

export 'capability_gen.dart';

/// 构建通道。发布构建不开放实验能力，且没有任何运行时开关能改这个事实。
enum BuildChannel { debug, release }

/// 一次能力判定的结果（UI 直接用 reason 解释为什么不可用）。
class CapabilityState {
  const CapabilityState({
    required this.key,
    required this.available,
    required this.reason,
    this.spec,
  });

  final String key;
  final bool available;
  final String reason;
  final CapabilitySpec? spec;

  bool get known => spec != null;
}

/// 能力门。
///
/// 判定顺序（fail-closed）：
///   1. 台账里没有 → 不可用（拼错 key 不能被当成"没限制"）；
///   2. disabled → 不可用，任何构建都一样；
///   3. experimental → 仅非发布构建可用；
///   4. beta/stable → 可用（各自功能仍要检查本机偏好与服务端状态）。
class CapabilityGate {
  CapabilityGate({BuildChannel? channel})
    : channel =
          channel ?? (kReleaseMode ? BuildChannel.release : BuildChannel.debug);

  /// 全局实例：UI 只读它，测试可以自行 new 一个指定通道的门。
  static final CapabilityGate instance = CapabilityGate();

  final BuildChannel channel;

  bool isAvailable(String key) => stateOf(key).available;

  CapabilityState stateOf(String key) {
    final spec = capabilitySpecOf(key);
    if (spec == null) {
      return CapabilityState(
        key: key,
        available: false,
        reason: '能力台账未登记 "$key"，按不可用处理',
      );
    }
    switch (spec.maturity) {
      case CapabilityMaturity.disabled:
        return CapabilityState(
          key: key,
          available: false,
          reason: spec.reason,
          spec: spec,
        );
      case CapabilityMaturity.experimental:
        if (channel == BuildChannel.release) {
          return CapabilityState(
            key: key,
            available: false,
            reason: '实验能力，发布构建不开放：${spec.reason}',
            spec: spec,
          );
        }
        return CapabilityState(
          key: key,
          available: true,
          reason: '实验能力（仅开发构建）：${spec.reason}',
          spec: spec,
        );
      case CapabilityMaturity.beta:
      case CapabilityMaturity.stable:
        return CapabilityState(
          key: key,
          available: true,
          reason: spec.reason,
          spec: spec,
        );
    }
  }
}
