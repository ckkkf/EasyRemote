# ADM-API-02a 验证记录（Overview.users_online）

> 卡：`docs/impl/tasks/ADM-API-02a-overview-users-online.md`
> 时间：2026-11-23 12:02-12:10（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | `Overview` 增 `users_online: int`（带语义注释） |
| `server/internal/contract/types_gen.go`、`web-admin/src/contract.ts`、`client/lib/contract.dart` | `node scripts/gen-contract.mjs` 生成物 |
| `server/internal/adminapi/adminapi.go` | Overview 统计在线设备时按 `account_id` 去重得到 `users_online` |
| `server/internal/adminapi/adminapi_test.go` | `TestOverviewUsersOnlineCountsDistinctAccounts` |
| `web-admin/src/api.ts` | `Overview` 类型加 `users_online` |
| `web-admin/src/features/dashboard/metrics.ts` | 在线用户改为真值；24h 新增设备仍 unavailable |
| `web-admin/tests/dashboard-metrics.test.ts` | 断言真值 + 24h 仍 null |
| `scripts/admin-ui-e2e-dashboard.py` | 在线用户=3、24h 新增设备=数据未提供 |

## 2. 字段映射

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `data.users_online` | int | 至少一台在线设备的账号数（去重） | 契约 `Overview`；presence 权威判定 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三端契约一致 | `gen-contract.mjs --check` | PASS（39 类型 / 38 接口） |
| 同账号两台在线设备只算 1 用户 | `TestOverviewUsersOnlineCountsDistinctAccounts` | PASS |
| 前端取真值、24h 仍 null | `TestMetricGroupsOnlyUseAuthoritativeFields` | PASS |
| 仪表盘显示真值 + 未提供项 | `TestDashboardE2E` | PASS 13/13 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（39 个类型 / 38 条接口）
--- PASS: TestOverviewUsersOnlineCountsDistinctAccounts (0.00s)
PASS users_online shows authoritative value -> 3
PASS devices_new_24h still unavailable
E2E OK (13 checks)
GATE: PASS
```

## 5. 未完成项、限制与风险

- `devices_new_24h` **没有做**：`store.Device` 无创建时间字段，需要设备表加列 + 迁移，已记在 ADM-API-02 的进度里。
- 在线用户依赖 presence 注入：未注入 presence 的开发装配会回落到设备表 `Online` 字段（与既有 `online()` 行为一致），生产必须注入。
- 契约变更走"先改 spec → 生成 → 实现"，没有手改生成物。

## 6. 回滚

见卡内回滚命令（`git checkout --` 契约、生成物、adminapi、前端类型/指标/测试、E2E 脚本）。
