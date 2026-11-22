import 'dart:io';

import 'package:easyremote_client/models.dart';
import 'package:easyremote_client/pages/settings_page.dart';
import 'package:easyremote_client/prefs.dart';
import 'package:easyremote_client/prefs_store.dart';
import 'package:easyremote_client/prefs_store_io.dart' as io_store;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// 回归：设置页的开关必须落盘（原来是纯 setState，刷新就回默认值）。
///
/// 两个层次分开验证，避免被 FakeAsync 的调度细节绊住：
///   1) UI 层：点开关 → 全局偏好总线（appPrefs）立即变 —— 证明 onChanged 接线正确；
///   2) 存储层：updatePrefsAwait → 文件里真的能读回来 —— 证明落盘路径正确。
/// testWidgets 跑在 FakeAsync 里，真实 File IO 必须经 tester.runAsync。
void main() {
  late Directory tmp;

  setUp(() async {
    tmp = await Directory.systemTemp.createTemp('er_prefs_test');
    io_store.prefsFilePathOverride = tmp.path + '/prefs.json';
    appPrefs.value = const Prefs();
  });

  tearDown(() async {
    io_store.prefsFilePathOverride = null;
    try {
      await tmp.delete(recursive: true);
    } catch (_) {}
  });

  Future<void> pumpSettings(WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            device: DeviceInfo(
              id: 'dev-1',
              code: '123456789',
              platform: 'macos',
              online: true,
            ),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 200));
  }

  testWidgets('隐私屏/防休眠未接入：没有开关，点了也不改偏好', (tester) async {
    await pumpSettings(tester);
    expect(appPrefs.value.privacyScreen, isFalse);
    expect(appPrefs.value.preventSleep, isTrue);

    expect(find.widgetWithText(SwitchListTile, '隐私屏'), findsNothing);
    expect(find.widgetWithText(SwitchListTile, '会话期间阻止睡眠'), findsNothing);

    final privacy = find.byKey(const Key('capability-privacy_screen'));
    await tester.scrollUntilVisible(privacy, 200);
    await tester.pumpAndSettle();
    await tester.tap(privacy, warnIfMissed: false);
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      appPrefs.value.privacyScreen,
      isFalse,
      reason: '未接入能力不得通过 UI 改写偏好，避免给人「已经生效」的错觉',
    );
    expect(appPrefs.value.preventSleep, isTrue);
  });

  testWidgets('同账号免密直连默认关，打开后写进偏好总线', (tester) async {
    await pumpSettings(tester);
    expect(appPrefs.value.sameAccountDirect, isFalse);

    final tile = find.widgetWithText(SwitchListTile, '同账号设备免密直连');
    expect(tile, findsOneWidget);
    expect(tester.widget<SwitchListTile>(tile).value, isFalse);

    await tester.tap(tile);
    await tester.pump(const Duration(milliseconds: 200));
    expect(appPrefs.value.sameAccountDirect, isTrue);
  });

  testWidgets('落盘后能读回（刷新后设置不会丢）', (tester) async {
    await tester.runAsync(() async {
      await updatePrefsAwait(
        const Prefs(
          themeMode: 'dark',
          quality: '流畅',
          privacyScreen: true,
          preventSleep: false,
        ),
      );
    });

    final saved = await tester.runAsync(() => loadPrefs());
    expect(saved, isNotNull);
    expect(saved!.themeMode, 'dark');
    expect(saved.quality, '流畅');
    expect(saved.privacyScreen, isTrue);
    expect(saved.preventSleep, isFalse);
  });

  testWidgets('重新进入设置页会读回已保存的值（模拟刷新后重进）', (tester) async {
    await tester.runAsync(() async {
      await updatePrefsAwait(
        const Prefs(
          privacyScreen: true,
          preventSleep: false,
          quality: '流畅',
          sameAccountDirect: true,
        ),
      );
    });

    await pumpSettings(tester);
    await tester.pump(const Duration(milliseconds: 200));

    expect(
      appPrefs.value.privacyScreen,
      isTrue,
      reason: '历史偏好仍要读回，等 ER-701 接入真实隐私屏后再恢复开关',
    );
    expect(appPrefs.value.preventSleep, isFalse);
    expect(appPrefs.value.sameAccountDirect, isTrue);
    final direct = find.widgetWithText(SwitchListTile, '同账号设备免密直连');
    expect(tester.widget<SwitchListTile>(direct).value, isTrue);
  });
}
