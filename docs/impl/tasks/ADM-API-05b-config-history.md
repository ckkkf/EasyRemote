# ADM-API-05b 配置变更历史 + 一键回滚

> 来源：`docs/impl/tasks/ADM-API-05-config-audit-contract.md`（变更历史、一键回滚）
> 需求：`docs/spec/pages.md` A12（行 258）、PRD §6.3.7（行 637「有变更历史、可一键回滚」）
> 前置：ADM-API-05a（审计筛选/动作集合）、ADM-UI-08a（配置 diff）

## 目标

1. `GET /admin/v1/config/history`：从审计（`config.update` / `config.rollback`）读变更历史，含 before/after JSON、actor、时间、结果；
2. `POST /admin/v1/config/rollback`：把单个键回滚到历史值，走同一套校验，写 `config.rollback` 审计（before/after 为单键 JSON）；
3. 管理端配置页展示历史卡片，并对可回滚项提供「回滚到 <旧值>」按钮。

## 输入 / 输出

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-15 `GET /admin/v1/config/history?limit=` | — | `ConfigHistoryResult{items: list<ConfigHistoryEntry>}` |
| ADM-16 `POST /admin/v1/config/rollback` | `{key, value}` | `ConfigPutResult{config, audited}` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：43 条端点一致。
- [ ] `TestConfigHistoryAndRollback`：历史含 config.update 且 after 有 600；ops 无 config.read 被拒；回滚后值=720、审计 `config.rollback` object=key、未知 key 被拒。
- [ ] `TestConfigAuditE2E`：历史卡片渲染、改 720→601 后出现历史、点「回滚到 720」值恢复且写审计、审计动作下拉出现 config.rollback。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/adminapi/config.go`、`config_history_test.go`（新增）
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-config-audit.py`
- `docs/impl/tasks/ADM-API-05b-config-history.md`、`docs/impl/evidence/ADM-API-05b/**`

禁止：手改生成物；用浏览器本地状态冒充服务端回滚；回滚不写审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'ConfigHistory' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-config-audit.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (16 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/route_specs/config.go/handler/前端/E2E/admin-scope；删除 `config_history_test.go` 与 `docs/impl/evidence/ADM-API-05b/`。
