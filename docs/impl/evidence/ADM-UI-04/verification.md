# ADM-UI-04 验证记录

> 卡：`docs/impl/tasks/ADM-UI-04.md`
> 时间：2026-11-23 11:05-11:15（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/features/dashboard/metrics.ts` | 指标口径唯一来源：`buildMetricGroups`（规模取 overview 真值，质量/稳定性全 null）、`POLL_INTERVAL_MS=5000`、`STALE_AFTER_MS=15000`、`isStale`、`formatUpdatedAt`、`onlineRate`（用 total 而不是分页样本） |
| `web-admin/src/App.tsx` | Dashboard 重构：三组指标、5s 轮询、暂停/刷新、更新于 + 陈旧标记、失败保留旧数据、趋势与下钻明确「接口未提供」 |
| `web-admin/tests/dashboard-metrics.test.ts` | 5 条口径断言 |
| `scripts/admin-ui-e2e-dashboard.py` | 12 条真实 E2E（轮询计数/暂停/陈旧/缺数据态） |
| `docs/impl/evidence/ADM-UI-00/page-matrix.md` | 追加变更记录 |

## 2. 字段映射

| 展示项 | 来源 |
|---|---|
| 在线设备/设备总数/进行中会话/待授权会话 | `Overview`（`contract.yaml`） |
| 在线用户 / 24h 新增设备 / 质量 6 项 / 稳定性 3 项 | 契约无字段 → 「数据未提供」，缺口卡 ADM-API-02 |
| 趋势 24h/7d/30d、平台/版本下钻 | 接口未提供 → 文案说明，不画曲线 |
| 最近会话/最近审计/平台分布 | `/admin/v1/sessions`、`/audit`、`/devices`（分布标注"本页样本"） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 规模取真值、质量/稳定性全 unavailable | `TestMetricGroupsOnlyUseAuthoritativeFields` | PASS |
| 缺数据不得写成 0 | `TestUnavailableMetricsAreNotZero` | PASS |
| 5s 刷新常量 | `TestPollIntervalMatchesPrd` | PASS |
| 陈旧阈值边界 | `TestStaleThreshold` | PASS |
| 在线率用权威总数 | `TestOnlineRateUsesAuthoritativeTotal` | PASS |
| 轮询/暂停/陈旧/缺数据态 | `TestDashboardE2E` | PASS（12/12） |
| 回归 / 技术栈 / 闸门 | `npm test`（29）/ `stack-check`（4/4）/ `gate.sh` | 全绿 |

## 4. 关键输出

```text
Tests  29 passed (29)
PASS polls every 5s -> 1 -> 2
PASS pause stops polling -> 2 -> 2
PASS stale badge appears when paused
PASS resume polls again -> 2 -> 3
E2E OK (12 checks)
GATE: PASS
```

## 5. 视觉基线与有意差异

与 ADM-UI-00 基线比对：**只有 a2 变化**，a1/a3/a6/a7/a12 判据完全一致。

```text
a2-dashboard.metricCards: 4 -> 15
a2-dashboard.metricLabels: [设备总数,在线设备,进行中会话,待授权] -> [在线设备,设备总数,进行中会话,待授权会话,在线用户,24h 新增设备,建连成功率,P2P 命中率,中继占比,延迟 P50,延迟 P95,帧率达成率,会话异常中断率,客户端崩溃率,重连成功率]
```

新基线记录：`docs/impl/evidence/ADM-UI-04/screens/manifest.json`（ADM-UI-00 的 manifest 保留为历史，不覆盖）。

## 6. 未完成项、限制与风险

- **质量/稳定性/趋势仍然没有数据源**：需要 telemetry（会话每秒采样、建连结果、崩溃上报）。这是 ADM-API-02 的 contract-first 工作，本卡只把界面做成诚实的"数据未提供"，没有造数。
- 平台分布只是当前页样本（`/admin/v1/devices` 分页），副标题已写明；全量分布需要后端聚合接口。
- 轮询仍是"每次拉 4 个列表接口"的粗粒度刷新（overview + devices + sessions + audit），迁 `useQuery` 合并/取消留到查询层卡；当前暂停/卸载都会清定时器，不会叠加请求。
- 已修一处口径错误：在线率原先用 `devices.length`（分页样本）当分母，现在用 `overview.devices_total`。
- 视觉细节（卡片密度、15 张卡的排版）留到 ADM-UI-10 统一收敛。

## 7. 回滚

删除 `web-admin/src/features/dashboard/metrics.ts`、`web-admin/tests/dashboard-metrics.test.ts`、`scripts/admin-ui-e2e-dashboard.py`、`docs/impl/evidence/ADM-UI-04/`；`git checkout -- web-admin/src/App.tsx`。
