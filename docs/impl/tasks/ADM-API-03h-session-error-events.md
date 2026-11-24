# ADM-API-03h 会话错误事件 + 错误码时间线

> 来源：`docs/impl/tasks/ADM-API-03h-session-errors.md`
> 需求：`docs/spec/pages.md` A8（行 240-242「错误码时间线」）
> 前置：ADM-API-03g（会话详情扩展）

## 目标

1. `session_errors` 表：记录会话级错误（code/name/detail/source/ts）；
2. 网关在**已知会话**的错误返回处落库（`sessions.ready`、`relay.ticket`）；
3. `GET /admin/v1/sessions/detail` 增加 `error_events`；
4. 详情页新增「错误码时间线」表（无错误显示空态）。

## 必须通过的断言

- [ ] `TestSessionErrorsRecorded`：未授权 ready → 拒绝且记录一条 `sessions.ready` 错误（name 非空）。
- [ ] `TestSessionsE2E`：对已结束会话调用 ready 制造真实错误 → 详情页错误码时间线 ≥1 行且含 `sessions.ready`。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `server/internal/stack/migrations/018_session_errors.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/gateway/{handler.go,session_errors.go,session_errors_test.go}`
- `server/internal/adminapi/adminapi.go`
- `docs/spec/contract.yaml`、三端生成物
- `web-admin/src/api.ts`、`features/sessions/session-detail-page.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-03h-session-error-events.md`、`docs/impl/evidence/ADM-API-03h/**`

禁止：错误写入失败覆盖原始错误响应；伪造错误事件；把"没有错误"显示成占位行。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/gateway/ -run 'TestSessionErrorsRecorded' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

三条断言全绿；E2E `E2E OK (38 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` store/gateway/adminapi/契约/生成物/前端/E2E；删除 018 迁移、`session_errors*.go` 与 `docs/impl/evidence/ADM-API-03h/`。
