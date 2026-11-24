# ADM-API-02e 仪表盘质量指标（P50/P95/中继占比 + 统计窗口）

> 来源：`docs/impl/tasks/ADM-API-02-dashboard-metrics-contract.md`（质量组）
> 需求：`docs/spec/pages.md` A2（行 204-207）、PRD §6.3.1（行 577）
> 前置：ADM-API-02c/02d（采样入站与聚合）、ADM-UI-04（仪表盘分组与「数据未提供」语义）

## 目标

给 `GET /admin/v1/stats/overview` 增加 `range=24h|7d|30d` 窗口与真实质量指标：`quality_available`、`latency_p50_ms`、`latency_p95_ms`、`relay_percent`、`quality_samples`，全部由客户端采样计算；无采样时 `quality_available=false`，前端继续显示「数据未提供」。

## 输入 / 输出

| 字段 | 类型 | 来源 |
|---|---|---|
| `quality_available` | bool | 窗口内采样数 > 0 |
| `latency_p50_ms` / `latency_p95_ms` | int | 采样延迟的最近秩分位数 |
| `relay_percent` | int | 采样中 relay=true 的比例 |
| `quality_samples` | int | 窗口内采样数 |

## 必须通过的断言

- [ ] `gen-contract --check`：三端一致（48 类型 / 46 接口）。
- [ ] `TestMemoryContract/QualityStatsSince`：P50/P95 最近秩、中继占比、空窗口。
- [ ] `TestOverviewQualityMetrics`：无采样 `quality_available=false`；有采样返回真实分位数。
- [ ] `TestDashboardE2E`：真实上报 3 条采样 → 页面显示 P50=34ms、P95=41ms、中继=66%；切 7d 时请求带 `range=7d`；切换后仍能回到 24h。
- [ ] `TestVisualBaselineUnchanged`：六屏判据不变（质量项顺序保持设计稿顺序）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`overview_quality_test.go`
- `server/internal/adminhttp/handler.go`（range 解析）
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`App.tsx`、`tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02e-overview-quality.md`、`docs/impl/evidence/ADM-API-02e/**`

禁止：手改生成物；无采样时填 0；把 P2P/建连/帧率达成率用推导值冒充真实指标。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/store/ ./internal/adminapi/ -run 'QualityStats|OverviewQuality' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (17 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E；删除 `overview_quality_test.go` 与 `docs/impl/evidence/ADM-API-02e/`。
