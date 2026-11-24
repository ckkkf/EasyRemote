# ADM-UI-09a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-09a.md`
> 时间：2026-11-23 11:41-11:45（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/tests/admin-scope.test.ts` | 4 条断言：路由=6 个有接口页面、无假导航、契约无 risk/version/admin 端点、缺口已建卡 |
| `docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md` | A10/A11/A14 所需端点与验收要求 |
| `docs/impl/evidence/ADM-UI-09/capability-audit.md` | 契约/服务端证据、页面需求对照、当前行为与后续 |
| `docs/impl/evidence/ADM-UI-00/page-matrix.md` | 变更记录 |

未改任何页面实现、`docs/spec/**`、`server/**`。

## 2. 字段映射

N/A（本卡不新增接口与字段）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 路由恰为 6 个有接口页面 | `TestAdminRoutesMatchImplementedPages` | PASS |
| 无假导航/占位入口 | `TestNoFakeAdminNavEntries` | PASS |
| 契约无 risk/versions/admins/roles/blacklist | `TestContractHasNoRiskVersionAdminEndpoints` | PASS |
| 缺口已建卡且覆盖 A10/A11/A14 | `TestUnopenedPagesDocumented` | PASS |
| 回归/闸门 | `npm test`（47）/ `tsc` / `build` / `gate.sh` | 全绿，`GATE: PASS` |

## 4. 关键输出

```text
Tests  47 passed (47)
$ grep -n 'path: /admin' docs/spec/contract.yaml
214:/admin/v1/login 220:/admin/v1/logout 226:/admin/v1/stats/overview
232:/admin/v1/users 238:/admin/v1/users/ban 244:/admin/v1/devices
250:/admin/v1/sessions 256:/admin/v1/sessions/kill 262:/admin/v1/config
268:/admin/v1/config 274:/admin/v1/audit
GATE: PASS
```

## 5. 未完成项、限制与风险

- A10/A11/A14 仍是**未开放**：没有告警/黑名单/爆破记录、版本/灰度/强制升级、管理员/角色/MFA 状态接口。任何"看起来能点"的入口都不存在，这是刻意选择。
- 这三页的优先级与交付形态（V1 分期、独立页面卡）需要负责人确认后再排期；本卡只锁定"不许做假入口"这条底线。
- 断言是白名单式：将来接口补齐后必须同步更新 `admin-scope.test.ts`，否则新增页面会被测试拦住——这是有意的摩擦。

## 6. 回滚

删除 `web-admin/tests/admin-scope.test.ts`、`docs/impl/evidence/ADM-UI-09/`、`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`。
