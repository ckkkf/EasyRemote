# ADM-API-02f3 验证记录（时间桶趋势）

> 卡：`docs/impl/tasks/ADM-API-02f3-trends.md`
> 时间：2026-11-23 15:36-15:47（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `TrendPoint`/`TrendsResult` + ADM-34 |
| `store/{store,memory,pg}.go` | `TrendBuckets`（UTC 对齐桶；PG 用 full outer join） |
| `adminapi/adminapi.go`、`trends_test.go` | `Trends`（range/bucket 校验） |
| `adminhttp/handler.go` | 路由 |
| `web-admin` 四文件 | `trends()` + `trendPath` 纯函数 + 趋势卡（三条 SVG 曲线） |
| `scripts/admin-ui-e2e-dashboard.py` | 23 条 E2E |

## 2. 口径

| 序列 | 来源 |
|---|---|
| 会话开始 | `sessions.started_at` |
| 建连尝试 | `connect_attempts.created_at` |
| 建连成功 | `connect_attempts.outcome='active'` |

桶按 UTC 对齐（`Truncate` / `floor(epoch/secs)`），24h 默认按小时、7d/30d 默认按天（前端按 range 选择）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（62 端点） |
| 桶聚合/参数/权限 | `TestTrendsBuckets` | PASS |
| 折线映射 | `TestTrendPathMapsBuckets` | PASS |
| 端到端 | `TestDashboardE2E` | PASS 23/23 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（64 个类型 / 62 条接口）
✓ 路由 method/path/auth 与契约一致（62 条端点）
--- PASS: TestTrendsBuckets
PASS trend buckets rendered -> 窗口 24h · 按小时 · 1 个时间桶
PASS trend polyline has points -> 160.0,2.0
PASS trend shows peak
E2E OK (23 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

内存实现 `CreateConnectAttempt` 原先无条件用 `time.Now()` 覆盖调用方给的时间，导致趋势测试的尝试落到"当前小时"而不是夹具时间。已改为"仅在未提供时取 now"，测试可注入时间。

## 6. 未完成项、限制与风险

- **未引入 Recharts**：用 SVG polyline 画折线（无新增依赖）；需要 tooltip/缩放/图例时再引图表库（PRD X5 目标）。
- 趋势只覆盖会话与建连两条事件源；质量曲线（延迟/丢包）的趋势未做。
- 空桶不补齐（只返回有数据的桶），前端因此显示"1 个时间桶"而不是 24 个——这是有意为之，避免伪造 0 值。
- 桶按 UTC 对齐，界面上没做时区换算（当前管理端一律展示 UTC 时间）。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 `trends_test.go` 与 `docs/impl/evidence/ADM-API-02f3/`。
