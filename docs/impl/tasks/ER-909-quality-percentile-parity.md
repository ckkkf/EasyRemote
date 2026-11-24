# ER-909 · 质量统计分位数与存储契约一致

成熟度：beta；不新增指标，不承诺采集闭环或提高远控成熟度。仅修 PostgreSQL 统计的 P50/P95，沿用既有共享测试明确的最近秩定义。

## 字段映射与能力链
- `Overview.latency_p50_ms` / `latency_p95_ms`（`docs/spec/contract.yaml:24–25`）：N/A，不改 DTO；统一两个 Store 对同一组样本的计算。
- 客户端会话采样 → `session_quality_samples` → `QualityStatsSince` → adminapi Overview → React。只修改统计查询，状态机、权限、采集、传输、UI 不改。

## 验收
- `TestPGContract/QualityStatsSince` 和 `TestMemoryContract/QualityStatsSince`：四个样本返回 P50=20/P95=40，空窗口无数据。沿用断言，不弱化。
- 红证据（PG 15）：`{Samples:4 LatencyP50Ms:25 LatencyP95Ms:39 RelayPercent:50}`。
- 验证真实 PG 完整契约、Go race 和严格 gate；回退只还原统计方法，无接口或 schema 迁移。

## 结果（2026-11-24）

真实 PostgreSQL 与 Memory 分位数回归、Go race、严格闸门均通过。未改变中继占比舍入和任何采样行为。
