// 偏好总线：全局单例 + 读出/写回。
//
// 设计取舍：
//   - 主题走 erThemeMode（ValueNotifier<ThemeMode>），因为它要被 MaterialApp 监听；
//   - 其余字段放 appPrefs（ValueNotifier<Prefs>），设置页直接读写；
//   - 任何一次修改都同步落盘，不依赖「退出时保存」（Web 关标签页没有退出回调）。
import 'dart:async';

import 'package:flutter/material.dart';

import 'prefs_store.dart';
import 'theme.dart';

export 'prefs_store_types.dart';

/// 当前生效的偏好（main() 启动时从本地读出后赋值）。
final ValueNotifier<Prefs> appPrefs = ValueNotifier<Prefs>(const Prefs());

ThemeMode themeModeFromPrefs(String value) => switch (value) {
  'light' => ThemeMode.light,
  'dark' => ThemeMode.dark,
  _ => ThemeMode.system,
};

String themeModeName(ThemeMode mode) => switch (mode) {
  ThemeMode.light => 'light',
  ThemeMode.dark => 'dark',
  ThemeMode.system => 'system',
};

/// 启动时调用：读本地偏好并应用（含主题）。
Future<void> loadAndApplyPrefs() async {
  final prefs = await loadPrefs();
  appPrefs.value = prefs;
  erThemeMode.value = themeModeFromPrefs(prefs.themeMode);
}

/// 改一项偏好：更新内存 → 同步主题（若是主题项）→ 落盘。
///
/// 不返回 Future：UI 回调不需要等磁盘。要确认写入完成（测试/退出前）用 updatePrefsAwait。
void updatePrefs(Prefs next) {
  appPrefs.value = next;
  erThemeMode.value = themeModeFromPrefs(next.themeMode);
  unawaited(savePrefs(next));
}

/// 同 updatePrefs，但返回落盘 Future。
Future<void> updatePrefsAwait(Prefs next) {
  appPrefs.value = next;
  erThemeMode.value = themeModeFromPrefs(next.themeMode);
  return savePrefs(next);
}

/// 切换外观（shell 的三段按钮/设置页开关都走这里，保证会落盘）。
void setThemeMode(ThemeMode mode) {
  erThemeMode.value = mode;
  final next = appPrefs.value.copyWith(themeMode: themeModeName(mode));
  appPrefs.value = next;
  unawaited(savePrefs(next));
}
