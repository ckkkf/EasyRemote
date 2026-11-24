# ADM-UI-09b A14 管理员与角色页面

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-09b）
> 需求：`docs/spec/pages.md` A14（行 268-272）、PRD §6.2（行 556-566）
> 前置：ADM-API-06a（`GET /admin/v1/admins`、`POST /admin/v1/admins/update`）

## 目标

把 A14 从"未开放"变成可用页面：管理员列表（用户名/角色/状态/MFA）、改角色、启用/禁用；写操作填原因并落审计；页面只对 `admin.read`（超管）可见，深链 403。

## 输入 / 输出

| 交互 | 接口 | 说明 |
|---|---|---|
| 列表 | `GET /admin/v1/admins?limit=50` | 用户名/角色/状态/MFA |
| 改角色 | `POST /admin/v1/admins/update {admin_id, role, reason}` | 原因必填 |
| 启停 | `POST /admin/v1/admins/update {admin_id, status, reason}` | 禁用后登录被拒（`Authenticate` status 检查） |

## 必须通过的断言

- [ ] `npm test -- --run`：路由表 7 项、`/admins` 受 `admin.read` 保护、导航只来自路由表。
- [ ] `TestAdminsE2E`：导航出现「管理员与角色」；列头正确；root=super/已开启、ops-dev=ops/禁用/未开启；改角色需原因并写 `admin.update` 审计；禁用唯一超管被 `ADMIN_FORBIDDEN` 拒绝且 root 仍启用；深链刷新正常；非超管看不到入口且深链显示无权限。
- [ ] 六屏视觉判据：仅 `a2-dashboard.nav` 增加一项（预期内），其余不变；新 manifest 作为基线。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `web-admin/src/app/routes.ts`、`src/api.ts`、`src/App.tsx`
- `web-admin/tests/{routes,admin-scope}.test.ts`
- `server/internal/stack/{stack_pg.go,seed_test.go}`（开发环境 seed 第二个管理员 `ops-dev`，disabled 且无 TOTP）
- `scripts/admin-ui-e2e-admins.py`（新增）
- `docs/impl/tasks/ADM-UI-09b-admins.md`、`docs/impl/evidence/ADM-UI-09b/**`

禁止：改契约（接口已在 06a 落地）；把角色/状态在本地乐观更新；给非超管显示入口。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-admins.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (13 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` routes/api/App/tests/stack seed；删除 `scripts/admin-ui-e2e-admins.py` 与 `docs/impl/evidence/ADM-UI-09b/`。
