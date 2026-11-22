# ER-003 证据：清理调试测试并修复 ABR 失败

- 状态: 完成
- PRD 依据: `docs/prd/easyremote-prd-v0.8.md:410`「升档保守（连续 3 周期达标），降档激进（单周期异常即降）」

## 变更

1. `core/crates/er-abr/src/lib.rs`：severe（loss>8% 或 queue>200）改为**当周期降档**；mild 保留两周期迟滞；健康升档仍 3 周期。`severe_streak` 保留诊断用途。
2. 单测：新增 `severe_single_cycle_downgrades`、`mild_single_cycle_does_not_downgrade`；修正固化旧行为的 `severe_loss_drops_two_levels`、`queue_backlog_is_severe`；`driver_downgrades_on_loss` 改为首份报告即降档。
3. 删除 `client/test/zz_dbg_test.dart`、`client/test/zz_plain_test.dart`（调试残留，不参与正式测试）。
4. `client/test/native_test.dart`：保留「单周期严重 → 降档」跨 FFI 断言；删除未使用 import。

## 结果

```
$ cd core && cargo test -p er-abr
13 passed
$ cd core && cargo test --workspace
全部 test result: ok（含 er-client 23 passed）
$ cd client && flutter test
+62: All tests passed!
$ bash scripts/gate.sh
GATE: PASS
```

日志：`flutter-test.log`、`docs/impl/evidence/ER-001/cargo-test-workspace.log`。

## 说明与回滚

- 旧的「单周期尖峰不降档」测试与 PRD 冲突，按 AGENTS「PRD 优先」改为 PRD 语义；不再保留旧断言。
- 回滚：恢复 controller 的两周期闸门会违反 PRD，禁止；若现场出现边界误降，回滚方式是调大 mild 阈值，而不是恢复 fail-open。
