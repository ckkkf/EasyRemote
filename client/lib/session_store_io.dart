// 桌面实现：登录令牌与设备身份写入系统安全存储。
//
// 旧版本把会话写成 ~/.easyremote/session.json 明文；现在只做一次性迁移：
// 安全存储没有数据时读取旧文件 → 写入安全存储 → 删除旧文件。
// 安全存储不可用时绝不回退明文（fail-closed：当作未登录）。
import 'dart:convert';
import 'dart:io';

import 'session_secret_store.dart';
import 'session_store_types.dart';

/// 测试/多实例隔离用：设置后作为旧版明文文件路径（默认 ~/.easyremote/session.json）。
String? sessionFilePathOverride;

/// 测试注入点：默认使用系统安全存储。
SecretStore sessionSecretStore = SystemSecretStore();

const String _sessionKey = 'easyremote.session.v1';

File _legacyFile() {
  final override = sessionFilePathOverride;
  if (override != null && override.isNotEmpty) return File(override);
  final home =
      Platform.environment['HOME'] ??
      Platform.environment['USERPROFILE'] ??
      '.';
  return File('$home/.easyremote/session.json');
}

/// 读取安全存储；存储不可用时抛错，由调用方按 fail-closed 处理。
Future<StoredSession?> _readSecure(SecretStore store) async {
  final raw = await store.read(_sessionKey);
  if (raw == null || raw.isEmpty) return null;
  try {
    return StoredSession.fromJson(jsonDecode(raw) as Map<String, dynamic>);
  } catch (_) {
    // 损坏的密文/结构：删除，要求重新登录。
    try {
      await store.delete(_sessionKey);
    } catch (e) {
      stderr.writeln('session: cleanup failed: $e');
    }
    return null;
  }
}

Future<StoredSession?> loadSession({SecretStore? store}) async {
  final secret = store ?? sessionSecretStore;
  StoredSession? secure;
  try {
    secure = await _readSecure(secret);
  } catch (_) {
    // 安全存储不可用：删掉旧明文，按未登录处理（绝不回退明文）。
    final legacy = _legacyFile();
    if (await legacy.exists()) {
      try {
        await legacy.delete();
      } catch (e) {
        stderr.writeln('session: cleanup failed: $e');
      }
    }
    return null;
  }
  if (secure != null) return secure;

  // 一次性迁移旧明文文件。
  final legacy = _legacyFile();
  if (!await legacy.exists()) return null;
  try {
    final parsed = StoredSession.fromJson(
      jsonDecode(await legacy.readAsString()) as Map<String, dynamic>,
    );
    if (parsed != null) {
      await secret.write(_sessionKey, jsonEncode(parsed.toJson()));
    }
    await legacy.delete();
    return parsed;
  } catch (_) {
    // 迁移失败：删除旧明文，避免长期停留；用户重新登录。
    try {
      await legacy.delete();
    } catch (e) {
      stderr.writeln('session: cleanup failed: $e');
    }
    return null;
  }
}

Future<void> saveSession(StoredSession session, {SecretStore? store}) async {
  final secret = store ?? sessionSecretStore;
  // 写入失败必须上抛：登录态不能“看起来成功”但实际没保存。
  await secret.write(_sessionKey, jsonEncode(session.toJson()));
  final legacy = _legacyFile();
  if (await legacy.exists()) {
    try {
      await legacy.delete();
    } catch (e) {
      stderr.writeln('session: cleanup failed: $e');
    }
  }
}

Future<void> clearSession({SecretStore? store}) async {
  final secret = store ?? sessionSecretStore;
  try {
    await secret.delete(_sessionKey);
  } catch (_) {
    // 注销必须尽力擦除；安全存储不可用时上抛，让调用方知道未擦净。
    rethrow;
  }
  final legacy = _legacyFile();
  if (await legacy.exists()) {
    try {
      await legacy.delete();
    } catch (e) {
      stderr.writeln('session: cleanup failed: $e');
    }
  }
}
