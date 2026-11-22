# ER-702 证据：防睡眠主路径

- 状态: 实现完成（能力 disabled，未真机验证）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| d2_power.rs | PlatformInhibitor（caffeinate/systemd-inhibit/SetThreadExecutionState） |
| host_stream.rs | keep_awake → SleepGuard 持有/释放 |
| er-ffi | er_host_start_stream 增加 keep_awake |
| Dart | HostService.start(keepAwake)、设备页传 prefs.preventSleep |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| guard Drop 释放 | guard_acquires_and_releases_on_drop | PASS |
| 提前 return 释放 | guard_releases_on_early_return | PASS |
| 平台激励器幂等/命令存在 | platform_tests | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- 未在 macOS/Windows/Linux 真机验证系统实际不睡眠。
- prevent_sleep 能力仍 disabled；UI 无开关，主路径只在 debug 调用时传 false。
- acquire 失败只写 last_error，未展示给用户。

## 4. 回滚

- 回滚主路径会恢复“电脑在远控中睡眠”，禁止；能力启用必须等真机验证。
