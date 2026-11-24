# ADM-API-05c2 审计 CSV 导出（脱敏 + 导出审计）

> 来源：`docs/impl/tasks/ADM-API-05c-config-scope-export.md`（拆分子卡 2）
> 需求：`docs/spec/pages.md` A13（行 264-266）、PRD §6.3.8（行 653「按模块导出 CSV，导出动作本身进审计」）
> 前置：ADM-API-05a（审计筛选）

## 目标

1. `GET /admin/v1/audit/export` 按当前筛选生成 CSV（服务端生成、服务端脱敏、RFC4180 转义）；
2. 响应仍走 JSON 信封（`AuditExportResult{csv, rows, truncated}`），不破坏「所有响应经 httpx.OK」约束；
3. 导出动作本身写 `audit.export`（含行数与截断标记）；
4. 前端「导出 CSV」按钮真实下载，并提示行数/截断。

## 脱敏与转义

| 项 | 规则 |
|---|---|
| IPv4 | 保留前两段：`203.0.113.42 → 203.0.x.x` |
| IPv4:port | 先拆端口再脱敏：`127.0.0.1:54321 → 127.0.x.x:54321` |
| IPv6 | 保留首段：`2001:db8::1 → 2001:****` |
| 含逗号/引号/换行 | 双引号包裹 + 引号翻倍（RFC4180） |
| 上限 | 5000 行；命中即 `truncated=true`，前端必须提示 |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：58 条端点一致。
- [ ] `TestExportAuditCSV`：行数/表头、IPv4 与 IPv6 脱敏、`IPv4:port` 脱敏、逗号转义、`audit.export` 审计、`limit=1` 截断、cs 角色被拒。
- [ ] `TestConfigAuditE2E`：导出按钮可用 → 真实下载 → CSV 表头正确 → IP 已脱敏（`127.0.x.x`）→ 页面提示已导出行数 → 审计出现 `audit.export`。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/adminapi/{adminapi,export_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-config-audit.py`
- `docs/impl/tasks/ADM-API-05c2-audit-export.md`、`docs/impl/evidence/ADM-API-05c2/**`

禁止：前端拼 CSV 绕过服务端脱敏；导出不写审计；把截断结果当完整导出。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'ExportAudit' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-config-audit.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (24 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E；删除 `export_test.go` 与 `docs/impl/evidence/ADM-API-05c2/`。
