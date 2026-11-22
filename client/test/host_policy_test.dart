import 'package:easyremote_client/host_policy.dart';
import 'package:easyremote_client/host_service.dart';
import 'package:easyremote_client/prefs.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('TestAllowControlDefaultsOff', () {
    const prefs = Prefs();
    expect(prefs.allowControl, isFalse);
    expect(
      hostBlockReason(capabilityAvailable: true, prefs: prefs),
      contains('关闭'),
    );
    expect(canStartHost(capabilityAvailable: true, prefs: prefs), isFalse);
  });

  test('TestDisabledHostRejectsRequest', () {
    // 即使历史偏好被写成 true，能力台账 disabled 时也不得启动。
    expect(
      hostBlockReason(
        capabilityAvailable: false,
        prefs: const Prefs(allowControl: true),
      ),
      contains('未接入'),
    );
    expect(
      canStartHost(
        capabilityAvailable: false,
        prefs: const Prefs(allowControl: true),
      ),
      isFalse,
    );

    appPrefs.value = const Prefs();
    expect(
      () => HostService.instance.prepare(
        gateway: 'http://127.0.0.1:1',
        relay: '127.0.0.1:2',
        token: '',
        deviceId: '',
        deviceCode: '',
        deviceToken: '',
      ),
      throwsA(isA<StateError>()),
      reason: 'HostService 必须在加载 FFI 之前就被门禁拒绝',
    );
  });
}
