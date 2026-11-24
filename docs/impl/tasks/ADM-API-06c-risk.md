# ADM-API-06c 风控黑名单与异常连接告警（A10 后端）

> 来源：`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`（A10 风控中心）
> 需求：`docs/spec/pages.md` A10（行 250）、PRD §6.3.5（行 609-617）
> 前置：ADM-API-04c（设备风险标记）、ADM-API-06a/06b

## 目标

落地 A10 的**可执行**部分：黑名单（账号 / 设备码 / IP，IP 支持 CIDR 与有效期）+ 连接闸门 + 异常连接告警。

- 连接闸门在 `/v1/connect/request` 真实生效：命中即拒绝（`CONN_RATE_LIMITED`）并写 `blacklist_block` 告警；
- 移除黑名单后同一请求恢复成功（可逆，有测试证明）。

**不做**：A10 页面、验证码爆破记录、举报工单、自动规则引擎（都需要独立设计）。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-26 `GET /admin/v1/risk/blacklist` | 分页 | `list<BlacklistRow>` | `risk.read` |
| ADM-27 `POST /admin/v1/risk/blacklist` | `{kind, value, note, ttl_hours?}` | `BlacklistRow` | `risk.write`（super/risk） |
| ADM-28 `POST /admin/v1/risk/blacklist/remove` | `{id, reason}` | `OkResult` | `risk.write` |
| ADM-29 `GET /admin/v1/risk/alerts` | 分页 | `list<RiskAlertRow>` | `risk.read` |

## 校验与语义

| 项 | 规则 |
|---|---|
| `kind` | `account` / `device` / `ip` |
| `value` | ip 必须是合法 IP 或 CIDR；其余非空 |
| `ttl_hours` | 0 表示永久；上限 8760 |
| 唯一性 | `kind+value` 唯一，重复加入 `ADMIN_CONFIG_INVALID` |
| 过期 | 过期条目不参与命中，也不删行（保留审计线索） |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：57 条端点一致。
- [ ] `TestMemoryContract/BlacklistAndAlerts`：重复冲突、CIDR 命中、过期不命中、移除、告警倒序。
- [ ] `TestBlacklistAdmin`：非法 IP/CIDR/kind 被拒、TTL、审计、重复、移除原因、ops 无写权限、readonly 可读告警。
- [ ] `TestConnectBlockedByAccountBlacklist`：命中账号黑名单 → 连接失败且 code=200018；告警落库；移除后恢复 200。
- [ ] 既有 gateway 全量测试 + web 55 tests + `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/008_risk_blacklist.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`risk_test.go`
- `server/internal/adminhttp/handler.go`
- `server/internal/gateway/{handler.go,risk_gate.go,risk_gate_test.go,helper_test.go}`
- `web-admin/tests/admin-scope.test.ts`
- `docs/impl/tasks/ADM-API-06c-risk.md`、`docs/impl/evidence/ADM-API-06c/**`

禁止：手改生成物；风控存储故障时静默放行；黑名单不写审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ ./internal/gateway/ -run 'Blacklist|ConnectBlocked' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && bash scripts/gate.sh
```

## 完成标准

五条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/gateway/admin-scope；删除 008 迁移、`risk_test.go`、`risk_gate*.go` 与 `docs/impl/evidence/ADM-API-06c/`。
