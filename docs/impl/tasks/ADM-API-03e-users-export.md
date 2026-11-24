# ADM-API-03e 用户时间范围筛选 + CSV 导出

> 来源：`docs/impl/tasks/ADM-API-03-users-contract.md`（A3 最后两项）
> 需求：`docs/spec/pages.md` A3（行 210-217）、PRD §6.3.2（行 585-592）、§6.3.8（行 653）
> 前置：ADM-API-03a..03d

## 目标

1. 用户列表支持**注册时间范围**过滤（`from`/`to`，服务端按 `created_at` 过滤，List 与 Count 同条件）；
2. `GET /admin/v1/users/export` 生成用户 CSV（服务端脱敏：手机号只出掩码），导出动作写 `user.export` 审计；
3. 前端「导出 CSV」真实下载 + 行数提示。

## 输入 / 输出

| 端点/参数 | 说明 |
|---|---|
| `GET /admin/v1/users?from=&to=` | 注册时间范围（RFC3339） |
| ADM-40 `GET /admin/v1/users/export` | `CsvExportResult{csv, rows, truncated}`，上限 5000 行 |
| 审计 | `user.export`，after 记 `rows=N;truncated=bool` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：68 条端点一致。
- [ ] `TestUsersTimeRangeAndExport`：窗口外账号被排除、窗口内的保留；CSV 表头正确、**不含完整手机号**、含掩码；`user.export` 审计；`limit=1` 截断。
- [ ] `TestUsersE2E`：未来时间范围 0 行、清空恢复；导出真实下载、CSV 掩码、审计 `user.export`。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/store/{memory,pg}.go`
- `server/internal/adminapi/{adminapi,users_export_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-users.py`
- `docs/impl/tasks/ADM-API-03e-users-export.md`、`docs/impl/evidence/ADM-API-03e/**`

禁止：前端拼 CSV 绕过服务端脱敏；导出不写审计；用客户端过滤冒充时间范围。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'UsersTimeRangeAndExport' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-users.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (26 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 `users_export_test.go` 与 `docs/impl/evidence/ADM-API-03e/`。
