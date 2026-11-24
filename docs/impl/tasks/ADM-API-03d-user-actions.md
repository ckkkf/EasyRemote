# ADM-API-03d 用户解封 / 踢下线

> 来源：`docs/impl/tasks/ADM-API-03-users-contract.md`（A3 剩余操作）
> 需求：`docs/spec/pages.md` A3（行 210-217）、PRD §6.3.2（行 585-592）
> 前置：ADM-API-03a/03b/03c（列表字段、设备、会话/备注）

## 目标

补齐 A3 的两个账号级动作，都要求原因并写审计：

1. `POST /admin/v1/users/unban` —— 解封（banned → active），已 active 时记 `noop` 审计；
2. `POST /admin/v1/users/kick` —— 踢下线：关闭该账号所有设备参与的未结束会话 + 设备置离线，审计记录 `closed_sessions/notify_failed/devices_offline`。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-38 `POST /admin/v1/users/unban` | `UserActionRequest{account_id, reason}` | `OkResult` | `user.ban`（super/risk） |
| ADM-39 `POST /admin/v1/users/kick` | 同上 | `OkResult` | `user.ban` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：67 条端点一致。
- [ ] `TestUnbanAndKickUser`：封禁→解封（状态回 active）；缺原因 `ADMIN_REASON_REQUIRED`；踢下线关闭目标账号会话、设备离线、**不影响其它账号**；审计含 `user.unban`/`user.kick`；readonly 被拒。
- [ ] `TestUsersE2E`：踢下线需原因且写审计；封禁后行变 banned、解封后恢复并写 `user.unban` 审计。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/adminapi/{adminapi,user_actions_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-users.py`
- `docs/impl/tasks/ADM-API-03d-user-actions.md`、`docs/impl/evidence/ADM-API-03d/**`

禁止：解封/踢下线不写审计；踢下线波及其它账号；前端本地改状态冒充服务端结果。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'UnbanAndKickUser' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-users.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (22 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E/admin-scope；删除 `user_actions_test.go` 与 `docs/impl/evidence/ADM-API-03d/`。
