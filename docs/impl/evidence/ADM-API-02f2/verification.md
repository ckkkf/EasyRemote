# ADM-API-02f2 验证记录（建连埋点 + 建连成功率）

> 卡：`docs/impl/tasks/ADM-API-02f2-connect-attempts.md`
> 时间：2026-11-23 15:26-15:37（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/011_connect_attempts.sql` | `connect_attempts`（session 唯一） |
| `store/{store,memory,pg}.go` | 创建尝试、标记 active/relay、窗口统计 |
| `docs/spec/contract.yaml`、生成物 | Overview 增 `connect_available / connect_attempts / connect_success_rate` |
| `gateway/handler.go`、`connect_attempt_test.go` | connect/request 记尝试；ready→active 记成功；relay 票据记 link |
| `adminapi/adminapi.go`、`connect_stats_test.go` | 窗口成功率 |
| `web-admin` 三文件 | 建连成功率真值 + 「尝试 N 次」提示 |
| `scripts/admin-ui-e2e-dashboard.py` | 21 条 E2E |

## 2. 口径

| 指标 | 分子 | 分母 | 数据来源 |
|---|---|---|---|
| 建连成功率 | outcome=active | 窗口内全部尝试 | `connect_attempts`（真实状态迁移） |
| P2P 命中率 | — | — | **不做**：服务端只能观察中继票据，不能反推 P2P |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（61 端点） |
| 无尝试/50% | `TestOverviewConnectRate` | PASS |
| 真实 ready→active | `TestConnectAttemptMarksActive` | PASS |
| 端到端 | `TestDashboardE2E` | PASS 21/21 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（62 个类型 / 61 条接口）
✓ 路由 method/path/auth 与契约一致（61 条端点）
--- PASS: TestOverviewConnectRate
--- PASS: TestConnectAttemptMarksActive
PASS connect success rate from real attempts -> 0%
PASS connect attempts count shown
PASS p2p hit rate still unavailable
E2E OK (21 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 未完成项、限制与风险

- **P2P 命中率仍缺**：需要客户端上报实际链路类型；服务端不用"没签票据"反推。
- 帧率达成率需要档位目标值（客户端上报目标帧率）。
- 埋点只覆盖 `/v1/connect/request` 这条入口；协助码等其它建连入口未接入。
- `connect_attempts` 无 TTL，长期增长；需要归档/清理卡。
- 尝试写入失败会让连接请求失败（fail-closed，避免统计失真）；这是有意选择，需在容量评估时考虑。

## 6. 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 011 迁移、`connect_attempt_test.go`、`connect_stats_test.go` 与 `docs/impl/evidence/ADM-API-02f2/`。
