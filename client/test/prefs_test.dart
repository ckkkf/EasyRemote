import 'package:easyremote_client/prefs_store_types.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('默认偏好：跟随系统 / 自动画质 / 隐私屏关 / 防睡眠开 / 允许被控开 / 免密直连关', () {
    const p = Prefs();
    expect(p.themeMode, 'system');
    expect(p.quality, '自动');
    expect(p.privacyScreen, isFalse);
    expect(p.preventSleep, isTrue);
    expect(p.allowControl, isTrue, reason: '默认要允许被控，否则首次远控永远连不上');
    expect(p.sameAccountDirect, isFalse, reason: '免密直连默认关');
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
