# ADM-UI-09c 验证记录（A10 风控中心页面）

> 卡：`docs/impl/tasks/ADM-UI-09c-risk.md`
> 时间：2026-11-23 14:26-14:34（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/app/routes.ts` | 新增 `risk` 路由（`/risk`，权限 `risk.read`） |
| `web-admin/src/api.ts` | `riskBlacklist/addBlacklist/removeBlacklist/riskAlerts` + 两个类型 |
| `web-admin/src/App.tsx` | `Risk` 页面：黑名单表 + 新增/移除对话框 + 告警表 |
| `web-admin/tests/{routes,admin-scope}.test.ts` | 路由 8 项、`/risk` 权限守卫、A10 移出"未开放" |
| `scripts/admin-ui-e2e-risk.py` | 14 条 E2E |
| `page-matrix.md` | A10 → implemented |

## 2. 字段映射

| 交互 | 接口字段 |
|---|---|
| 列表 | `BlacklistRow{id, kind, value, note, expires_at?, created_at, created_by}` |
| 新增 | `{kind, value, note, ttl_hours}` → `BlacklistRow` |
| 移除 | `{id, reason}` → `OkResult` |
| 告警 | `RiskAlertRow{id, kind, subject, detail, ts}` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 路由表/权限守卫 | `TestAdminRouteTableCoversNav`、`TestPermissionGateOnlyConfig` | PASS |
| A10 已开放且受 `risk.read` | `TestAdminRoutesMatchImplementedPages`、`TestNoFakeAdminNavEntries` | PASS |
| 端到端（含真实拦截告警） | `TestRiskE2E` | PASS 14/14 |
| 六屏判据 | `TestVisualBaselineUnchanged` | 仅 nav 增加一项（预期），新 manifest 为基线 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
PASS nav shows risk page
PASS blocked connect produced alert -> True
PASS invalid ip rejected -> 配置值不合法 | 700010 ADMIN_CONFIG_INVALID · trace ...
PASS blacklist row added -> account acc-... 同 IP 高频连接 2026/11/24 14:28:14 root 移除
PASS add writes audit
PASS alert listed from real block -> 1
PASS alert subject is account
PASS remove requires reason
PASS remove writes audit
PASS no risk nav without permission
PASS forbidden on deep link
E2E OK (14 checks)
VISUAL DIFF: a2-dashboard.nav 增加 '⛨ 风控中心'
GATE: PASS
```

## 5. 未完成项、限制与风险

- A10 的验证码爆破记录、举报工单仍未做（需要独立数据模型）。
- 页面为单页列表（limit=50），未接分页控件。
- 拦截告警目前只来自 `/v1/connect/request` 的闸门；其它入口（协助码、文件传输）尚未接入。
- 视觉基线本次**有意**变更（导航多一项），新基线：`docs/impl/evidence/ADM-UI-09c/screens/manifest.json`。

## 6. 回滚

`git checkout --` routes/api/App/tests；删除 `scripts/admin-ui-e2e-risk.py` 与 `docs/impl/evidence/ADM-UI-09c/`；矩阵 A10 回退为 blocked-by-api。
