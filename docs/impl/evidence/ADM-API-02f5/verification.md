# ADM-API-02f5 / 02f6 验证记录（健康上报 + 24h 新增设备）

> 卡：`docs/impl/tasks/ADM-API-02f5-health-report.md`
> 时间：2026-11-23 18:04-18:16（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/014_health_events.sql` | `client_health_events` |
| `server/internal/stack/migrations/015_device_created_at.sql` | `devices.created_at` |
| `store/{store,memory,pg}.go` | `RecordHealthEvent` / `HealthStatsSince` / `CountDevicesSince`、设备 `CreatedAt` |
| `docs/spec/contract.yaml`、生成物、两端 `route_specs.go` | GW-27 + Overview 六个新字段 |
| `gateway/handler.go` | `POST /v1/sessions/health`（kind 白名单 + 会话双方校验） |
| `adminapi/adminapi.go`、`health_stats_test.go` | 崩溃率 / 重连成功率 / 24h 新增设备 |
| `web-admin` 三文件 | 三个指标改为真值 |
| `scripts/admin-ui-e2e-dashboard.py` | 25 条 E2E |

## 2. 口径

| 指标 | 定义 |
|---|---|
| 崩溃率 | 崩溃事件 / 窗口内开始的会话数 ×100 |
| 重连成功率 | reconnect_success / reconnect_attempt |
| 24h 新增设备 | `devices.created_at >= now-24h` 的数量 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（69 端点） |
| 无事件/100%/50% | `TestOverviewHealthMetrics` | PASS |
| 端到端 | `TestDashboardE2E` | PASS 25/25 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 矩阵 | `check-admin-ui-matrix` | PASS（A 行 11 implemented / 3 partial） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（68 个类型 / 69 条接口）
✓ 路由 method/path/auth 与契约一致（69 条端点）
--- PASS: TestOverviewHealthMetrics
PASS crash rate from reported events -> 100%
PASS reconnect success rate from reported events -> 50%
PASS devices new 24h from device created_at -> 4
E2E OK (25 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

1. `deviceCols` 加列后，两处 `rows.Scan(...)` 的右括号被批量替换吃掉，导致 `pg.go` 语法错误；已逐个修复并 `go build` 验证。
2. `metrics.ts` 去掉最后一个 `unavailable(...)` 调用后，`unavailable` helper 变成未使用（TS6133）；已删除。
3. 单测仍断言 `devices_new_24h === null`；改为断言真值 6（fixture）。

## 6. 未完成项、限制与风险

- **客户端尚未真正上报 health 事件**；E2E 用设备凭证模拟，真实崩溃/重连数据要等客户端接入。
- `devices.created_at` 由迁移回填为迁移时刻：**历史设备会被算进"24h 新增"一次**，之后恢复正常；文档已注明。
- 崩溃率分母是"窗口内开始的会话数"，同一会话多次崩溃会 >100%；如需按会话去重，需要额外的去重规则。
- A7/A8 的延迟/链路列仍未做（链路数据已入库，但 SessionRow 未带该字段，属 UI/契约小改）。

## 7. 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 014/015 迁移、`health_stats_test.go` 与 `docs/impl/evidence/ADM-API-02f5/`。
