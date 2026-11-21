# ADM-02 用户列表 · 信封收口

> 来源：PRD v0.8 §6.3.2（行 586–593）、§6.4.1（行 675–698）
> 契约：`docs/spec/contract.yaml` → `ADM-02` / `UserRow`

## 目标

把 `GET /admin/v1/users` 的成功响应从 `{"users":[...]}` 改为统一信封 `{code,name,data,trace_id}`，数据放进 `data.items`。

## 输入 / 输出

请求：`GET /admin/v1/users`，Header `Authorization: Bearer <admin session>`

响应（成功，HTTP 200）：

| 字段 | 类型 | 说明 | 来源 |
|---|---|---|---|
| code | int | 固定 0 | PRD §6.4.1 行 690「后端改字段，前端必须跟着改」 |
| name | string | 固定 "OK" | 同上 |
| data.items[].id | string | 账号 ID | PRD §6.3.2 行 589「详情」 |
| data.items[].phone_mask | string | 形如 138****8888 | PRD §6.3.2 行 593「手机号默认脱敏」 |
| data.items[].status | string | active / banned | PRD §6.3.2 行 590「封禁状态」 |
| data.items[].created_at | time | RFC3339 | PRD §6.3.2 行 589「注册时间」 |
| data.total | int | 总数（当前一次取 200，见 adminapi.collectAccounts） | — |
| trace_id | string | 与 X-Trace-Id 一致 | error-codes.md §3 |

响应（失败）：沿用错误信封，码取 `ADMIN_SESSION_EXPIRED` / `ADMIN_FORBIDDEN`，禁止自造。

## 必须通过的断言

- [ ] `TestAdminEnvelopeContract`：ADM-02 不再出现在违规列表；`ER_ENVELOPE_STRICT=1` 时该卡相关项全绿
- [ ] `TestListUsers_Envelope`：`code==0`、`data.items` 为数组、`trace_id` 非空
- [ ] `TestListUsers_Mask`：`phone_mask` 匹配 `^[0-9]{3}\*{4}[0-9]{4}$`
- [ ] `TestListUsers_Empty`：空库返回 `items: []` 而不是 `null`
- [ ] `TestListUsers_Forbidden`：role=readonly 且无 user.read 权限时返回 `ADMIN_FORBIDDEN`

## 只准改这些文件

- `server/internal/adminapi/adminapi.go`（仅 `ListUsers` 及其返回类型）
- `server/internal/adminhttp/handler.go`（仅 `/admin/v1/users` 这一段）
- `server/internal/adminapi/adminapi_test.go`（新增断言）
- `web-admin/src/api.ts`（users 改读 `body.data.items`）
- `docs/impl/prd-traceability.md`（状态与验证方式）

禁止：改 `docs/spec/contract.yaml` 的字段名、改 `httpx` 包、动其它端点。

## 验收命令

`@bash
cd server && go test ./internal/adminapi/ ./internal/adminhttp/ -run 'ListUsers|Envelope' -v
cd .. && bash scripts/gate.sh
`@

## 完成标准

断言全绿 + gate PASS + 回复里贴出上面两条命令的完整输出。
