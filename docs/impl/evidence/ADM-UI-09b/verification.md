# ADM-UI-09b 验证记录（A14 管理员与角色页面）

> 卡：`docs/impl/tasks/ADM-UI-09b-admins.md`
> 时间：2026-11-23 14:15-14:26（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/app/routes.ts` | 新增 `admins` 路由（`/admins`，权限 `admin.read`） |
| `web-admin/src/api.ts` | `admins()` / `updateAdmin()` + `AdminRow` 类型 |
| `web-admin/src/App.tsx` | `Admins` 页面：列表/改角色/启停 + 原因对话框；注册路由 |
| `web-admin/tests/routes.test.ts`、`admin-scope.test.ts` | 路由表 7 项、`/admins` 受权限保护、A14 不再列入"未开放" |
| `server/internal/stack/stack_pg.go`、`seed_test.go` | 开发环境 seed `ops-dev`（disabled、无 TOTP），并断言生产不 seed |
| `scripts/admin-ui-e2e-admins.py` | 13 条 E2E |
| `docs/impl/evidence/ADM-UI-00/page-matrix.md` | A14 → implemented |

## 2. 字段映射

| 交互 | 接口字段 |
|---|---|
| 列表 | `AdminRow{id, username, role, status, mfa_enabled}` |
| 改角色 | `{admin_id, role, reason}` → `OkResult` |
| 启停 | `{admin_id, status, reason}` → `OkResult` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 路由表/权限守卫 | `TestAdminRouteTableCoversNav`、`TestPermissionGateOnlyConfig` | PASS |
| A14 已开放且受 `admin.read` | `TestAdminRoutesMatchImplementedPages`、`TestNoFakeAdminNavEntries` | PASS |
| 开发 seed（含生产不 seed） | `TestDevSeedsDevAdmin`、`TestProductionNeverSeedsDevAdmin` | PASS |
| 端到端 | `TestAdminsE2E` | PASS 13/13 |
| 六屏判据 | `TestVisualBaselineUnchanged` | 仅 nav 增加一项（预期），新 manifest 为基线 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
PASS nav shows admin page
PASS admin columns -> ['用户名', '角色', '状态', 'MFA', '操作']
PASS root is super with mfa -> super
PASS ops-dev is ops and disabled without mfa -> ops
PASS role change requires reason
PASS role change writes audit
PASS last super self-disable blocked -> 没有权限执行该操作 | 700005 ADMIN_FORBIDDEN · trace ...
PASS non-super sees no admin nav
PASS non-super gets forbidden
E2E OK (13 checks)
VISUAL DIFF: a2-dashboard.nav 增加 '☗ 管理员与角色'
GATE: PASS
```

## 5. 过程中的两次自我纠正

1. 首版 E2E 用 `innerText` 判断角色 —— select 会把所有 option 文本算进去，断言形同虚设；改为读 `select.input_value()`。
2. 首版"防锁死"断言在 body 里找"超管"，而页面副标题本来就含这三个字 —— 恒真。改为定位 `[role=alert]` 并断言 `ADMIN_FORBIDDEN`。

## 6. 未完成项、限制与风险

- 新建管理员 / 重置密码 / MFA 重置仍未做（06a 已记录）。
- 页面只做单页列表（limit=50），未接服务端分页控件；管理员数量增长后需要分页卡。
- 开发 seed 增加 `ops-dev`（disabled、无 TOTP、不可登录）；生产环境被 `IsProduction` 整段拦住，并有测试守护。
- 视觉基线本次**有意**变更（导航多一项），新 manifest 位于 `docs/impl/evidence/ADM-UI-09b/screens/manifest.json`。

## 7. 回滚

`git checkout --` routes/api/App/tests/stack seed；删除 `scripts/admin-ui-e2e-admins.py` 与 `docs/impl/evidence/ADM-UI-09b/`；矩阵 A14 回退为 blocked-by-api。
