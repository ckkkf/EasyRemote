# ADM-API-03a 验证记录（UserRow.device_count）

> 卡：`docs/impl/tasks/ADM-API-03a-user-device-count.md`
> 时间：2026-11-23 12:06-12:14（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | `UserRow` 增 `device_count: int` |
| 三端生成物 | `node scripts/gen-contract.mjs`（未手改） |
| `server/internal/store/{store,memory,pg}.go` | 新增 `DeviceCountsByAccount`（一次聚合，避免 N+1） |
| `storetest/storetest.go` | `DeviceCountsByAccount` 契约用例 |
| `server/internal/adminapi/adminapi.go` | ListUsers 填充 `device_count` |
| `adminapi_test.go` | `TestListUsers_DeviceCount` |
| `web-admin/src/api.ts`、`App.tsx`、`features/users/user-detail-page.tsx` | 类型 + 列表「设备数」列 + 详情展示 |
| `scripts/admin-ui-e2e-users.py` | 6 列断言 + 服务端设备数断言 |

## 2. 字段映射

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `data.items[].device_count` | int | 账号已绑定设备数 | 契约 `UserRow`；`DeviceCountsByAccount` 一次聚合 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三端契约一致 | `gen-contract --check` | PASS（39 类型 / 38 接口） |
| 多账号计数/无设备账号/空入参 | `TestMemoryContract/DeviceCountsByAccount` | PASS |
| acc1=2、acc2=0 | `TestListUsers_DeviceCount` | PASS |
| 列表 6 列 + 设备数 2/1 | `TestUsersE2E` | PASS 13/13 |
| 视觉判据 | `TestVisualBaselineUnchanged` | 仅 a3 列变化（有意） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（39 个类型 / 38 条接口）
--- PASS: TestMemoryContract/DeviceCountsByAccount (0.00s)
--- PASS: TestListUsers_DeviceCount (0.00s)
PASS users table columns -> ['账号 ID', '手机号', '设备数', '状态', '注册时间', '操作']
PASS device count column from server -> ['2', '1']
E2E OK (13 checks)
a3-users.columns: [...5 列] -> [...6 列]   # 有意变化
GATE: PASS
```

## 5. 未完成项、限制与风险

- `nickname` / `last_login_at` **未做**：账号表无这两列，需要 schema 迁移（加列 + 回填策略），已记在 ADM-API-03。
- 设备数是一次聚合查询（`WHERE account_id = ANY($1)`），页大小上限 500 时也只有一次往返。
- 视觉差异仅 a3 列；a2 指标顺序保持基线不变（本轮曾误调整顺序，已还原）。

## 6. 回滚

见卡内 `git checkout --` 清单（契约、三端生成物、store、adminapi、前端、E2E 脚本）。
