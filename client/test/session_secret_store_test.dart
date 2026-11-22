import 'dart:convert';
import 'dart:io';

import 'package:easyremote_client/session_secret_store.dart';
import 'package:easyremote_client/session_store_io.dart';
import 'package:easyremote_client/session_store_types.dart';
import 'package:flutter_test/flutter_test.dart';

class MemorySecretStore implements SecretStore {
  final Map<String, String> data = {};
  Object? readError;
  Object? writeError;

  @override
  Future<String?> read(String key) async {
    if (readError != null) throw readError!;
    return data[key];
  }

  @override
  Future<void> write(String key, String value) async {
    if (writeError != null) throw writeError!;
    data[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    data.remove(key);
  }
}

const session = StoredSession(
  token: 'acc-token',
  deviceToken: 'dev-token',
  deviceId: 'dev-id',
  deviceCode: '123456789',
  devicePlatform: 'macos',
);

void main() {
  late Directory tmp;
  late String legacyPath;

  setUp(() async {
    tmp = await Directory.systemTemp.createTemp('er-secret-');
    legacyPath = '${tmp.path}/session.json';
    sessionFilePathOverride = legacyPath;
  });

  tearDown(() async {
    sessionFilePathOverride = null;
    if (await tmp.exists()) await tmp.delete(recursive: true);
  });

  test('TestRestartKeepsIdentity', () async {
    final store = MemorySecretStore();
    await saveSession(session, store: store);
    // 模拟重启：同一安全存储，新的读取。
    final restored = await loadSession(store: store);
    expect(restored?.token, 'acc-token');
    expect(restored?.deviceToken, 'dev-token');
    expect(restored?.deviceId, 'dev-id');
    expect(restored?.deviceCode, '123456789');
  });

  test('TestLogoutErasesIdentity', () async {
    final store = MemorySecretStore();
    await saveSession(session, store: store);
    await clearSession(store: store);
    expect(await loadSession(store: store), isNull);
    expect(store.data, isEmpty);
    expect(await File(legacyPath).exists(), isFalse);
  });

  test('TestLegacyPlaintextMigratesAndIsDeleted', () async {
    final store = MemorySecretStore();
    await File(legacyPath).writeAsString(jsonEncode(session.toJson()));
    final restored = await loadSession(store: store);
    expect(restored?.token, 'acc-token');
    expect(store.data.values.single, contains('acc-token'));
    expect(await File(legacyPath).exists(), isFalse);
  });

  test('TestSecureStoreFailureFailsClosed', () async {
    final store = MemorySecretStore()
      ..readError = StateError('keychain locked');
    await File(legacyPath).writeAsString(jsonEncode(session.toJson()));
    expect(await loadSession(store: store), isNull);
    expect(
      await File(legacyPath).exists(),
      isFalse,
      reason: '安全存储不可用时不能继续保留明文',
    );
  });

  test('TestSecureStoreWriteFailureSurfaces', () async {
    final store = MemorySecretStore()
      ..writeError = StateError('keychain locked');
    await expectLater(saveSession(session, store: store), throwsStateError);
  });
}
