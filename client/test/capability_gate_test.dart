// ER-000：能力门必须 fail-closed。测试名与任务卡验收断言一一对应。
import 'package:easyremote_client/capabilities.dart';
import 'package:easyremote_client/prefs_store_types.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('TestIncompleteCapabilityIsDisabled', () {
    final release = CapabilityGate(channel: BuildChannel.release);
    // 实验能力：发布构建一律不可用，即使本机偏好打开了允许被控。
    expect(release.isAvailable('remote_control'), isFalse);
    expect(release.isAvailable('file_transfer'), isFalse);
    expect(
      release.isAvailable('quality_metrics'),
      isFalse,
      reason: '质量指标里仍有未接入字段，发布构建不把整块当完成态',
    );

    // disabled 能力：连开发构建都不给入口。
    final debug = CapabilityGate(channel: BuildChannel.debug);
    expect(debug.isAvailable('privacy_screen'), isFalse);
    expect(debug.isAvailable('prevent_sleep'), isFalse);
    expect(debug.isAvailable('prevent_sleep'), isFalse);
    expect(debug.isAvailable('clipboard_sync'), isFalse);

    // 实验能力只在开发构建可见（便于继续整改/E2E），发布构建不可越过。
    expect(debug.isAvailable('remote_control'), isTrue);
    expect(debug.isAvailable('file_transfer'), isFalse);
    expect(debug.isAvailable('remote_host'), isTrue, reason: '本地授权已接入，开发构建可验证');
    expect(debug.isAvailable('remote_control'), isTrue);
    expect(
      debug.isAvailable('file_transfer'),
      isFalse,
      reason: 'A-14：文件尚未走会话 data stream，先隐藏',
    );
    expect(release.isAvailable('remote_host'), isFalse, reason: '发布构建仍不开放');
  });

  test('TestUnknownCapabilityFailsClosed', () {
    final gate = CapabilityGate(channel: BuildChannel.debug);
    final state = gate.stateOf('typo_capability');
    expect(state.available, isFalse);
    expect(state.known, isFalse);
    expect(state.reason, contains('未登记'));
  });

  test('TestPreferenceCannotUnlockExperimentalInRelease', () {
    // 偏好里 allow_control=true 是历史默认值；能力门不读它，发布构建仍然拒绝。
    const prefs = Prefs(allowControl: true);
    expect(prefs.allowControl, isTrue);
    final release = CapabilityGate(channel: BuildChannel.release);
    expect(release.isAvailable('remote_host'), isFalse);
    expect(release.isAvailable('remote_control'), isFalse);
  });

  test('默认通道来自构建类型：测试环境是 debug', () {
    expect(CapabilityGate().channel, BuildChannel.debug);
  });
}
