# ER-013 证据：身份语义统一与跨设备拒绝

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 契约变更

| 变更 | 说明 |
|---|---|
| GW-08 status | GET + bearer；账号必须拥有会话一方 |
| GW-10 keys | GET + bearer；同上 |
| GW-22 DELETE /v1/files/ | 新增，FileDeletedResult |
| GW-23 POST /v1/files/ | 新增（下载票据），FileTicketResult |
| GW-19 reflect | GET（ER-012 已改） |

文件子资源用集合前缀表达；RouteSpec 三条同 path 不同 method，MethodGuard 前缀白名单允许 GET/DELETE/POST，具体子路径由 handler 校验。

## 2. 服务端鉴权

- status：account → 会话存在 → ownsDevice(acc, controller|target)，否则 700005。
- keys：account + ownsDevice（原有逻辑，保持不变）。
- consent：requireDevice(target)（原有逻辑）。
- ticket：requireDevice(controller|target)（原有逻辑）。
- 文件下载/删除/取票：文件归属账号 + 设备凭证（原有逻辑，现补契约与 method）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 无关账号读 status/keys → 403 | TestCrossDeviceAccessRejected | PASS |
| 无关账号 consent/ticket → 403 | 同上 | PASS |
| 无关账号下载/删除/取票 → 403/404 | 同上 | PASS |
| 会话双方可读 status | 同上 | PASS |
| 文件子资源方法合法（DELETE/POST 前缀） | TestEveryRouteRejectsWrongMethod（35 端点） | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/gateway/ -run TestCrossDeviceAccessRejected -v
    --- PASS
    $ node scripts/check-routes.mjs --strict
    ✓ 路由 method/path/auth 与契约一致（35 条端点）
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界 / 回滚

- MethodGuard 前缀白名单对同一前缀下所有子路径生效；细粒度子路径语义仍由 handler 保证，后续可升级为参数化路由表。
- status 从 device 凭证改为账号 bearer + 归属校验，与契约一致；核心/客户端均带 bearer，E2E 已回归。
- 回滚会重新暴露“知道 session_id 即可读状态”的风险，禁止作为长期状态。
