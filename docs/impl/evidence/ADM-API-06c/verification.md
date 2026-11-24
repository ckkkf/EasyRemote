# ADM-API-06c 验证记录（风控黑名单 + 连接闸门 + 告警）

> 卡：`docs/impl/tasks/ADM-API-06c-risk.md`
> 时间：2026-11-23 14:04-14:17（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/008_risk_blacklist.sql` | `blacklist_entries`（kind+value 唯一）+ `risk_alerts` |
| `store/{store,memory,pg}.go`、`storetest` | 黑名单增删查、`FindActiveBlacklist`、`MatchIPBlacklist`（CIDR）、告警读写 |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `BlacklistRow`/`BlacklistAddRequest`/`BlacklistRemoveRequest`/`RiskAlertRow` + ADM-26..29 |
| `adminapi/adminapi.go`、`risk_test.go` | 黑名单校验（IP/CIDR/kind/TTL）、审计、告警列表 |
| `adminhttp/handler.go` | 四条路由 |
| `gateway/handler.go`、`gateway/risk_gate.go`、`risk_gate_test.go` | 连接闸门：命中即拒绝 + 写告警 |
| `gateway/helper_test.go` | 修掉夹具把 `AccountID` 覆盖成空串的缺陷（见下） |
| `web-admin/tests/admin-scope.test.ts` | 白名单加 /risk，移出禁止名单 |

## 2. 字段映射

| 字段/端点 | 说明 |
|---|---|
| `BlacklistRow{id, kind, value, note, expires_at?, created_at, created_by}` | 过期条目不删，仅不参与命中 |
| `BlacklistAddRequest{kind, value, note, ttl_hours?}` | kind∈account/device/ip；ip 支持 CIDR；ttl 0=永久，上限 8760 |
| `RiskAlertRow{id, kind, subject, detail, ts}` | 目前仅 `blacklist_block` |
| 闸门 | `/v1/connect/request` 命中 → `CONN_RATE_LIMITED(200018)` + 告警 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（57 端点） |
| 冲突/CIDR/过期/移除/告警排序 | `TestMemoryContract/BlacklistAndAlerts` | PASS |
| 校验/审计/权限 | `TestBlacklistAdmin` | PASS |
| 闸门拦截 + 告警 + 可逆 | `TestConnectBlockedByAccountBlacklist` | PASS |
| gateway 全量回归 | `go test ./internal/gateway/` | PASS |
| web 回归 | `npm test -- --run` | PASS 55/55 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（57 个类型 / 57 条接口）
✓ 路由 method/path/auth 与契约一致（57 条端点）
--- PASS: TestMemoryContract/BlacklistAndAlerts
--- PASS: TestBlacklistAdmin
--- PASS: TestConnectBlockedByAccountBlacklist
GATE: PASS
```

## 5. 过程中修掉的真实缺陷

**测试夹具把 AccountID 覆盖成空串**：`helper_test.go` 的 `loginAs` 先解码登录响应（含 `account_id`），随后又用 `/v1/devices/bind` 响应覆盖 —— 而 `BindResult` 没有 `account_id` 字段，导致所有夹具账号的 `AccountID` 恒为空。本轮黑名单测试因此一开始"命中不了"。已删除该覆盖行，gateway 全量测试仍全绿。

## 6. 未完成项、限制与风险

- **A10 页面未做**；验证码爆破记录、举报工单未做（需要独立数据模型与流程）。
- 闸门在**风控存储故障时拒绝连接**（fail-closed），错误码为 `SRV_INTERNAL`；若运维希望降级放行，需要显式配置项，当前不做。
- 拦截错误码复用 `CONN_RATE_LIMITED`（200018），文案为 "blocked by risk control"；新增专用码需要改 `error-codes.md` 并重生成三端，留待负责人批准。
- CIDR 匹配在 Go 侧遍历 ip 类条目，条目多时需改为索引/前缀树。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/gateway/admin-scope；删除 008 迁移、`risk_test.go`、`risk_gate*.go` 与 `docs/impl/evidence/ADM-API-06c/`。
