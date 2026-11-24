# ADM-API-02a Overview 增补 users_online（契约先行）

> 来源：`docs/impl/tasks/ADM-API-02-dashboard-metrics-contract.md`（规模组缺口）
> 需求：`docs/spec/pages.md` A2（行 201-208）、PRD §6.3.1（行 576）
> 前置：ADM-UI-04（仪表盘已把缺字段显示为「数据未提供」）

## 目标

契约先行补上 `Overview.users_online`（至少有一台在线设备的账号数，按账号去重），服务端用权威 presence 判定计算，管理端仪表盘从「数据未提供」切成真值。

**不做** `devices_new_24h`：`store.Device` 没有创建时间字段，算不出来；要补需要设备表加列 + 迁移，单独排期（已记在 ADM-API-02）。

## 输入 / 输出

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `data.users_online` | int | 至少一台在线设备的账号数（去重） | 契约 `Overview`；服务端按 presence 判定的在线设备集合去重 `account_id` |

顺序：改 `docs/spec/contract.yaml` → `node scripts/gen-contract.mjs` → Go 实现 → 前端消费。

## 必须通过的断言

- [ ] `node scripts/gen-contract.mjs --check`：三端契约一致（39 类型 / 38 接口）。
- [ ] `TestOverviewUsersOnlineCountsDistinctAccounts`：同账号两台在线设备只算 1 个用户；离线设备不计。
- [ ] `TestMetricGroupsOnlyUseAuthoritativeFields`：`users_online` 取真值，`devices_new_24h` 仍为 null。
- [ ] `TestDashboardE2E`：`users_online` 显示 3（夹具 3 账号），`devices_new_24h` 显示「数据未提供」，其余 11 条检查不回归。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`
- `server/internal/contract/types_gen.go`、`web-admin/src/contract.ts`、`client/lib/contract.dart`（生成物）
- `server/internal/adminapi/adminapi.go`（Overview）、`adminapi_test.go`
- `web-admin/src/api.ts`（Overview 类型）、`web-admin/src/features/dashboard/metrics.ts`
- `web-admin/tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02a-overview-users-online.md`、`docs/impl/evidence/ADM-API-02a/**`

禁止：手改生成物；改 `httpx`；把 `devices_new_24h` 用假值凑出来。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/adminapi/ -run Overview -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout -- docs/spec/contract.yaml server/internal/contract/types_gen.go web-admin/src/contract.ts client/lib/contract.dart server/internal/adminapi/adminapi.go server/internal/adminapi/adminapi_test.go web-admin/src/api.ts web-admin/src/features/dashboard/metrics.ts web-admin/tests/dashboard-metrics.test.ts scripts/admin-ui-e2e-dashboard.py`。
