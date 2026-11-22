# ER-010 证据：路由检查器 method/path/auth 三方对齐

- 状态: 完成（`node scripts/check-routes.mjs --strict` 通过；`bash scripts/gate.sh` → GATE: PASS）

## 1. 交付

| 文件 | 目的 |
|---|---|
| `server/internal/httpx/routespec.go` | `RouteSpec{ID,Method,Path,Auth}` 类型（运行时闸门在 ER-012/013 复用） |
| `server/internal/gateway/route_specs.go` | 27 条网关端点声明（含 SYS-01） |
| `server/internal/adminhttp/route_specs.go` | 11 条后台端点声明 |
| `scripts/check-routes.mjs` | 解析契约 + 代码注册 + RouteSpec，按 path+method 对齐；--strict 任一漂移退出 1 |
| `scripts/tests/check-routes.test.mjs` | 4 条 fixture 自测 |

## 2. 字段映射

| 声明字段 | contract.yaml |
|---|---|
| RouteSpec.ID | endpoint id（GW-xx/ADM-xx/SYS-01） |
| RouteSpec.Method | `method:` |
| RouteSpec.Path | `path:` |
| RouteSpec.Auth | `auth: none/bearer/device/admin` |

## 3. 判定规则

- 契约端点 ↔ 代码声明：按 `path + method` 匹配；缺声明、ID 不符、auth 不符都报。
- 代码注册 ↔ 契约：path 集合双向核对（幽灵接口/未登记路由）。
- 声明 ↔ 代码注册：声明必须真的注册了 handler。
- 同一 path 多方法合法（`/admin/v1/config` GET+PUT）；同 path+method 重复报 duplicate。
- 报告模式（默认）只打印；`--strict` 判红（gate/CI 使用）。

## 4. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| method 漂移被发现 | `TestRouteCheckerRejectsMethodDrift` | PASS |
| auth 漂移被发现 | `TestRouteCheckerRejectsAuthDrift` | PASS |
| path 幽灵/未登记/缺声明 | `TestRouteCheckerRejectsPathDrift` | PASS |
| 完全一致时零 issue | `TestRouteCheckerAcceptsMatchingRoutes` | PASS |

真实漂移验证：把 GW-01 声明改成 GET，`--strict` exit 1（日志 `method-drift.log`），恢复后通过。

## 5. 命令与输出

```
$ node --test scripts/tests/check-routes.test.mjs
4 tests pass
$ node scripts/check-routes.mjs --strict
✓ 路由 method/path/auth 与契约一致（32 条端点）
$ bash scripts/gate.sh
  scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
GATE: PASS
```

## 6. 边界与后续

- 本卡只保证「契约 ↔ 代码声明 ↔ 注册路径」一致；**运行时**仍由各 handler 内部鉴权，方法闸门/405 信封由 **ER-012** 落地，GW-08 的 GET/POST 与 bearer/device 语义由 **ER-013** 修正。
- RouteSpec 目前是声明表，不是运行时中间件；ER-012 必须直接消费它，禁止再维护第二份 method/auth 表。
- 回滚：删除 spec 文件与 checker 升级即可回到 path-only；不得用放宽 checker 的方式让漂移通过。
