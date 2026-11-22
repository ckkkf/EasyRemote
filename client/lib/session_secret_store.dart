import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// 敏感会话材料的存储抽象。
///
/// 生产使用系统安全存储（macOS Keychain / Windows Credential Manager /
/// Linux libsecret）。测试可注入内存实现；任何安全存储错误都必须失败，
/// 绝不允许回退到明文文件或 localStorage。
abstract class SecretStore {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
  Future<void> delete(String key);
}

/// 系统安全存储实现。
class SystemSecretStore implements SecretStore {
  SystemSecretStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) =>
      _storage.write(key: key, value: value);

  @override
  Future<void> delete(String key) => _storage.delete(key: key);
}
