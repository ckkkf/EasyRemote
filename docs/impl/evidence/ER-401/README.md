# ER-401 证据：安全存储

- 状态: 完成（桌面安全存储 + 迁移/擦除；gate PASS）
- 限制：Web 仍 localStorage；Windows/Linux 真机验证归 ER-900。

## 1. 变更

| 文件 | 目的 |
|---|---|
| pubspec.yaml | flutter_secure_storage ^11.2.0 |
| session_secret_store.dart | SecretStore + SystemSecretStore |
| session_store_io.dart | 安全存储读写、旧文件迁移、fail-closed |
| test/session_secret_store_test.dart | 5 条安全存储测试 |
| test/session_restore_test.dart | 断言改为安全存储内容 |
| test/flutter_test_config.dart | 测试注入内存 SecretStore |

## 2. 语义

- 保存：写安全存储成功后删除旧明文；写入失败上抛。
- 读取：安全存储优先；为空时迁移旧明文（写安全存储→删旧文件）。
- 安全存储不可用：删除旧明文、返回未登录；绝不回退明文。
- 注销：删除安全存储 key + 旧文件；失败上抛。
- 损坏数据：删除并返回未登录。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 重启身份不变 | TestRestartKeepsIdentity | PASS |
| 注销不可恢复 | TestLogoutErasesIdentity | PASS |
| 旧明文迁移并删除 | TestLegacyPlaintextMigratesAndIsDeleted | PASS |
| 安全存储失败 fail-closed | TestSecureStoreFailureFailsClosed | PASS |
| 保存失败上抛 | TestSecureStoreWriteFailureSurfaces | PASS |
| 会话恢复/过期/断网 | session_restore_test 6 条 | PASS |

## 4. 命令与输出

    $ cd client && flutter test test/session_secret_store_test.dart test/session_restore_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- Web 仍使用 localStorage（session_store_web.dart）；需独立威胁模型/CSP/CSRF 方案（ER-902）。
- 测试通过 flutter_test_config 注入内存实现；生产默认 SystemSecretStore。
- macOS Keychain entitlement/签名、Windows Credential Manager、Linux libsecret 未在本机真机验证（ER-900）。
- API 的 save/clear 未区分平台错误类型；UI 由错误信封/日志提示。

## 6. 回滚

- 回滚到明文文件属于安全红线退化，禁止。
- 回滚迁移会让旧 token 永久停留明文，禁止。
