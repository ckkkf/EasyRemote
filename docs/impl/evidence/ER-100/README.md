# ER-100 证据：allow_control 默认关闭与迁移

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 行为

- Prefs 增加 schemaVersion=2；toJson 写 version=2。
- fromJson：无 version 或 version<2 时 allow_control 强制 false（旧版默认 true 不能当同意）；version>=2 的显式 true 保留。
- host_policy.hostBlockReason：prefs.allowControl=false → “允许被控已关闭”；capability remote_host 不可用 → “被控能力尚未接入”。
- HostService.prepare/start 在 _load() 之前调用门禁，直接调用也无法绕过 UI。

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 默认与缺字段 false | TestAllowControlDefaultsOff | PASS |
| 旧快照 true 迁移为 false，其它偏好保留 | TestLegacyAllowControlMigratesToFalse | PASS |
| 门禁拒绝 prepare（FFI 前） | TestDisabledHostRejectsRequest | PASS |
| 能力未接入时即使偏好 true 也拒绝 | TestDisabledHostRejectsRequest（policy 分支） | PASS |

## 3. 命令与输出

    $ cd client && flutter test test/prefs_test.dart test/host_policy_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 边界 / 归属

- 服务端“被控端未启动/不在线时连接请求必须失败”没有在本卡实现：HTTP API-only 会话（ErSessionRunner）不经过信令绑定，简单 fail-closed 会误杀。该项归 ER-111（presence + notifier 语义），代码中已留 TODO。
- 能力 remote_host 仍为 disabled（ER-102 实现本地授权后才允许启用），因此当前任何构建都无法真正开启被控。

## 5. 回滚

- 回滚 schema 会让旧 true 重新生效，属于 fail-open，禁止。
- 回滚 HostService 门禁会让内部调用绕过 UI 启动被控，禁止。
