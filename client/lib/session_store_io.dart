// 桌面实现：写到 ~/.easyremote/session.json（仅当前用户可读）。
// 页面/网络层通过条件导入选择本文件或 session_store_web.dart，不要直接 import dart:io。
import 'dart:convert';
import 'dart:io';

import 'session_store_types.dart';

/// 测试/多实例隔离用：设置后优先于默认路径（默认 ~/.easyremote/session.json）。
String? sessionFilePathOverride;

File _file() {
  final override = sessionFilePathOverride;
  if (override != null && override.isNotEmpty) return File(override);
  final home = Platform.environment['HOME'] ?? Platform.environment['USERPROFILE'] ?? '.';
  return File('$home/.easyremote/session.json');
}

Future<StoredSession?> loadSession() async {
  final file = _file();
  if (!await file.exists()) return null;
  try {
    return StoredSession.fromJson(
        jsonDecode(await file.readAsString()) as Map<String, dynamic>);
  } catch (_) {
    try {
      await file.delete();
    } catch (_) {}
    return null;
  }
}

Future<void> saveSession(StoredSession session) async {
  final file = _file();
  await file.parent.create(recursive: true);
  await file.writeAsString(jsonEncode(session.toJson()), flush: true);
  if (!Platform.isWindows) {
    // 令牌等价于账号，文件权限收到本人可读写。
    try {
      await Process.run('chmod', <String>['600', file.path]);
    } catch (_) {}
  }
}

Future<void> clearSession() async {
  final file = _file();
  try {
    if (await file.exists()) await file.delete();
  } catch (_) {}
}
