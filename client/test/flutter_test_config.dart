import 'dart:async';

import 'package:easyremote_client/session_secret_store.dart';
import 'package:easyremote_client/session_store_io.dart';

/// 测试环境没有平台 Keychain 通道：统一注入内存实现。
/// 生产默认仍然是 SystemSecretStore（见 session_store_io.dart）。
class _MemorySecretStore implements SecretStore {
  final Map<String, String> data = {};

  @override
  Future<String?> read(String key) async => data[key];

  @override
  Future<void> write(String key, String value) async => data[key] = value;

  @override
  Future<void> delete(String key) async => data.remove(key);
}

Future<void> testExecutable(FutureOr<void> Function() testMain) async {
  sessionSecretStore = _MemorySecretStore();
  await testMain();
}
