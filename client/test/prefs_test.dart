import 'package:easyremote_client/prefs_store_types.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('默认偏好：跟随系统 / 自动画质 / 隐私屏关 / 防睡眠开 / 允许被控关 / 免密直连关', () {
    const p = Prefs();
    expect(p.themeMode, 'system');
    expect(p.quality, '自动');
    expect(p.privacyScreen, isFalse);
    expect(p.preventSleep, isTrue);
    expect(p.allowControl, isFalse, reason: '安全红线：首次安装默认不允许被控');
    expect(p.sameAccountDirect, isFalse, reason: '免密直连默认关');
  });

  test('TestLegacyAllowControlMigratesToFalse', () {
    // 旧版本没有 version 字段，且当年默认 allow_control=true；迁移后必须是 false。
    final legacy = Prefs.fromJson(<String, dynamic>{
      'theme_mode': 'dark',
      'allow_control': true,
    });
    expect(legacy.allowControl, isFalse, reason: '历史默认开不能当成本人同意');
    expect(legacy.themeMode, 'dark', reason: '其它偏好仍按原值保留');

    // 新版本快照里的显式 true 才保留。
    final current = Prefs.fromJson(<String, dynamic>{
      'version': Prefs.schemaVersion,
      'allow_control': true,
    });
    expect(current.allowControl, isTrue);
    expect(Prefs.fromJson(const Prefs().toJson()).allowControl, isFalse);
  });

  test('TestAllowControlDefaultsOff', () {
    // 默认值、缺字段、脏数据三种来源都必须是 false（禁止 fail-open）。
    expect(const Prefs().allowControl, isFalse);
    expect(Prefs.fromJson(<String, dynamic>{}).allowControl, isFalse);
    expect(
      Prefs.fromJson(<String, dynamic>{'allow_control': 'yes'}).allowControl,
      isFalse,
    );
    expect(
      Prefs.fromJson(<String, dynamic>{'allow_control': true}).allowControl,
      isFalse,
      reason: '无 version 的旧快照按历史默认值迁移为 false',
    );
  });

  test('JSON 往返不丢字段（刷新后能还原用户改过的值）', () {
    const p = Prefs(
      themeMode: 'dark',
      quality: '流畅',
      privacyScreen: true,
      preventSleep: false,
      allowControl: false,
      sameAccountDirect: true,
    );
    final back = Prefs.fromJson(p.toJson());
    expect(back.themeMode, 'dark');
    expect(back.quality, '流畅');
    expect(back.privacyScreen, isTrue);
    expect(back.preventSleep, isFalse);
    expect(back.allowControl, isFalse);
    expect(back.sameAccountDirect, isTrue);
  });

  test('脏数据不炸启动：未知枚举值/缺字段回落默认', () {
    final back = Prefs.fromJson(<String, dynamic>{
      'theme_mode': 'neon',
      'quality': '8K',
      'privacy_screen': 'yes',
    });
    expect(back.themeMode, 'system');
    expect(back.quality, '自动');
    expect(back.privacyScreen, isFalse);
    expect(back.preventSleep, isTrue);
  });

  test('copyWith 只改目标字段', () {
    const p = Prefs(themeMode: 'light');
    final q = p.copyWith(quality: '高清');
    expect(q.themeMode, 'light');
    expect(q.quality, '高清');
    expect(q.preventSleep, isTrue);
  });
}
