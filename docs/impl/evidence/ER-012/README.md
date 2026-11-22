# ER-012 证据：method guard 与统一 405

- 状态: 完成（`bash scripts/gate.sh` → GATE: PASS）

## 1. 交付

| 文件 | 目的 |
|---|---|
| `docs/spec/error-codes.md` | 新增 700012 SRV_METHOD_NOT_ALLOWED（405） |
| `server/internal/httpx/methodguard.go` | 按 RouteSpec 校验 method；错误时写 Allow + 契约错误信封 |
| `gateway/handler.go`、`adminhttp/handler.go` | Routes() 用 MethodGuard 包住 mux |
| `route_specs.go` | 补 SYS-01（admin /healthz）；按契约重新生成 |
| `contract.yaml` | GW-08/GW-10/GW-19 改为 GET；新增 GW-21 GET /v1/files（FileListResult） |
| 测试 | gateway/adminhttp 表驱动 + 真实服务集成 |

## 2. 行为

- 已声明路径 + 错误 method → HTTP 405，信封 `{code:700012,name:SRV_METHOD_NOT_ALLOWED,...}`，`Allow: GET, POST` 等。
- 未声明路径 → 仍由 JSONMux 返回 700011 404。
- 允许方法来自 RouteSpec，与 `check-routes.mjs` 的契约对照是同一份声明。
- 子路径 `/v1/files/{id}` 的 DELETE/ticket 尚未进契约，暂由 handler 处理；ER-013 登记。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 网关 33 条端点拒绝 PATCH/TRACE | `TestEveryRouteRejectsWrongMethod` | PASS |
| 真实 Routes 生效（非只测中间件） | `TestGatewayRoutesRejectWrongMethod` | PASS |
| 后台每条路由拒绝错误 method | `TestEveryAdminRouteRejectsWrongMethod` | PASS |

## 4. 命令与输出

```
$ cd server && go test ./internal/gateway/ ./internal/adminhttp/ ./internal/httpx/
ok

$ node scripts/check-routes.mjs --strict
✓ 路由 method/path/auth 与契约一致（33 条端点）

$ bash scripts/gate.sh
  scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
GATE: PASS
```

## 5. 边界 / 回滚

- GW-08/GW-10/GW-19 的契约由 POST 改成 GET，是让契约描述现有真实调用；ER-013 仍需统一身份凭证语义（bearer/device）。
- 405 是 fail-closed：新增路由必须先写契约+RouteSpec，否则不会被 guard 覆盖。
- 回滚会重新允许错误 method 进入业务逻辑，禁止作为长期状态。
