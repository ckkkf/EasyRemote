// 桌面实现：写到 ~/.easyremote/prefs.json（与 session.json 同目录）。
// 页面/网络层通过条件导入选择本文件或 prefs_store_web.dart，不要直接 import dart:io。
import 'dart:convert';
import 'dart:io';

import 'prefs_store_types.dart';

/// 测试/多实例隔离用：设置后优先于默认路径（默认 ~/.easyremote/prefs.json）。
String? prefsFilePathOverride;

File _file() {
  final override = prefsFilePathOverride;
  if (override != null && override.isNotEmpty) return File(override);
  final home = Platform.environment['HOME'] ?? Platform.environment['USERPROFILE'] ?? '.';
  return File(home + '/.easyremote/prefs.json');
}

Future<Prefs> loadPrefs() async {
  final file = _file();
  if (!await file.exists()) return const Prefs();
  try {
    return Prefs.fromJson(jsonDecode(await file.readAsString()) as Map<String, dynamic>);
  } catch (_) {
    return const Prefs();
  }
}

Future<void> savePrefs(Prefs prefs) async {
  final file = _file();
  await file.parent.create(recursive: true);
  await file.writeAsString(jsonEncode(prefs.toJson()), flush: true);
}
