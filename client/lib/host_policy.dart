import 'prefs_store_types.dart';

/// 被控端启动前的统一门禁。
///
/// 两道条件缺一不可：
///   1. 本机偏好 allow_control 必须由用户显式打开（默认 false，历史缺字段也是 false）；
///   2. 能力台账里 remote_host 必须可用（C18 本地授权弹窗落地前是 disabled）。
///
/// UI 隐藏开关只是第一道防线；HostService 必须在真正调 FFI 前再校验一次，
/// 防止其他入口/旧代码路径绕过界面直接启动被控。
String? hostBlockReason({
  required bool capabilityAvailable,
  required Prefs prefs,
}) {
  if (!prefs.allowControl) return '允许被控已关闭（本机设置）';
  if (!capabilityAvailable) return '被控能力尚未接入（缺本地授权弹窗）';
  return null;
}

bool canStartHost({required bool capabilityAvailable, required Prefs prefs}) =>
    hostBlockReason(capabilityAvailable: capabilityAvailable, prefs: prefs) ==
    null;
