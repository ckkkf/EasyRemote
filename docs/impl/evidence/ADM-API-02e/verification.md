# ADM-API-02e 验证记录（仪表盘质量指标）

> 卡：`docs/impl/tasks/ADM-API-02e-overview-quality.md`
> 时间：2026-11-23 13:14-13:26（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物 | Overview 增 `quality_available/latency_p50_ms/latency_p95_ms/relay_percent/quality_samples` |
| `store/{store,memory,pg}.go`、`storetest` | `QualityStatsSince`（最近秩分位数；PG 用 `percentile_cont`） |
| `adminapi/adminapi.go`、`overview_quality_test.go` | `OverviewSince`（窗口化概览 + 质量指标） |
| `adminhttp/handler.go` | `range=24h|7d|30d` 解析 |
| `web-admin` 四文件 | 质量卡真值 + 统计窗口选择器（请求带 range） |
| `scripts/admin-ui-e2e-dashboard.py` | 真实采样 → 断言 P50/P95/中继；窗口切换断言 |

## 2. 字段映射

| 字段 | 来源 |
|---|---|
| `quality_available` | 窗口内采样数 > 0 |
| `latency_p50_ms`/`latency_p95_ms` | 最近秩分位数（PG `percentile_cont`） |
| `relay_percent` | relay=true 占比 |
| `quality_samples` | 窗口内采样数 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约一致 | `gen-contract --check` | PASS（48 类型 / 46 接口） |
| 分位数/中继/空窗口 | `TestMemoryContract/QualityStatsSince` | PASS |
| available 语义 + 真值 | `TestOverviewQualityMetrics` | PASS |
| 端到端 + 窗口切换 | `TestDashboardE2E` | PASS 17/17 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（48 个类型 / 46 条接口）
--- PASS: TestMemoryContract/QualityStatsSince
--- PASS: TestOverviewQualityMetrics
PASS quality p50 from samples -> 34 ms
PASS quality p95 from samples -> 41 ms
PASS quality relay percent from samples -> 66%
PASS range switch hits server with range param
E2E OK (17 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 过程中修掉的问题

- 质量项顺序一度被改动导致视觉判据漂移；已恢复设计稿顺序（建连/P2P/中继/P50/P95/帧率），判据回归一致。
- E2E 夹具曾因"设备码随机 + 按 code 排序"选到主控自己触发自连接 400；改为按 code 显式排除主控，两次连续运行均通过。

## 6. 未完成项、限制与风险

- 建连成功率、P2P 命中率、帧率达成率仍无数据源（需要建连埋点与档位目标值）；**不用推导值冒充**。
- 稳定性组（异常中断率/崩溃率/重连成功率）与 24h/7d/30d 趋势图仍缺。
- 分位数在内存实现是最近秩法、PG 是 `percentile_cont`，两者在小样本下可能有 ±1ms 差异；口径已写进测试注释。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E；删除 `overview_quality_test.go` 与 `docs/impl/evidence/ADM-API-02e/`。
