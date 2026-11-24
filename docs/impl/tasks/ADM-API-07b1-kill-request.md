# ADM-API-07b1 强制中断双人确认（后端）

> 来源：`docs/impl/tasks/ADM-UI-07b-kill-double-confirm.md`（拆分子卡 1）
> 需求：`docs/spec/pages.md` A9（行 244-246）、PRD §6.3.4（行 606）
> 前置：ADM-API-05a（审计筛选）、现有 `session.Kill` 单次中断

## 目标

把"高危中断需要第二人确认"做成真实工作流：第一人发起请求（5 分钟有效）→ 第二人确认后才真正 `session.Kill`。

- 同一人不能既发起又确认；
- 过期请求不能确认（不能复用几小时前的批准）；
- 已关闭会话不能再发起；
- 发起、确认、真正中断三条审计都在。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-31 `POST /admin/v1/sessions/kill-request` | `{session_id, reason}` | `KillRequestRow` | `session.kill` |
| ADM-32 `POST /admin/v1/sessions/kill-confirm` | `{request_id}` | `KillSessionResult` | `session.kill`（且必须是另一人） |
| ADM-33 `GET /admin/v1/sessions/kill-requests?status=pending` | — | `list<KillRequestRow>` | `session.read` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：61 条端点一致。
- [ ] `TestKillRequestTwoPersonFlow`：发起→同人确认被拒→第二人确认后 closed→重复确认 `SESSION_EXPIRED`→已关闭会话再发起被拒→三条审计齐全。
- [ ] `TestKillRequestExpires`：TTL 5 分钟后确认失败。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/010_kill_requests.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/adminapi/{adminapi,kill_request_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/tests/admin-scope.test.ts`
- `docs/impl/tasks/ADM-API-07b1-kill-request.md`、`docs/impl/evidence/ADM-API-07b1/**`

禁止：同一人确认；过期请求仍可确认；确认后不写 `session.kill` 审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'KillRequest' -v
cd .. && bash scripts/gate.sh
```

## 完成标准

四条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 010 迁移、`kill_request_test.go` 与 `docs/impl/evidence/ADM-API-07b1/`。
