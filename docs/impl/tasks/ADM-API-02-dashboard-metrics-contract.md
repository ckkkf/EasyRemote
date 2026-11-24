# ADM-API-02 仪表盘指标与会话质量 telemetry 契约

> 来源：ADM-UI-04/06a/07a 执行时确认的缺口；`docs/spec/pages.md` A2（行 201-208）、A6（行 229-232）、A8（行 240-242）、PRD §6.3.1（行 570-583）、§6.3.3（行 597-601）、§6.3.4（行 603-607）
> 性质：**contract-first 后端卡**。管理端已把相关指标显示为「数据未提供」，本卡负责把数据真正做出来。

## 缺口

| 需求 | 现状 | 需要的契约变化 |
|---|---|---|
| 规模：在线用户数、24h 新增设备 | 无字段 | `Overview` 增 `users_online`、`devices_new_24h` |
| 质量：建连成功率、P2P 命中率、中继占比、延迟 P50/P95、帧率达成率 | 无字段、无采样 | 新增 `GET /admin/v1/stats/quality?range=24h|7d|30d&platform=&version=`，返回指标 + `available` 标记 |
| 稳定性：异常中断率、崩溃率、重连成功率 | 无字段 | 同上接口的 stability 分组 |
| 趋势 24h/7d/30d、平台/版本下钻 | 无接口 | 同上接口按 range/platform/version 聚合 |
| 设备 30 天质量曲线 | 无接口 | `GET /admin/v1/devices/{id}/quality?range=30d` |
| 会话每秒质量曲线 / 链路切换 / 错误码时间线 | 无采样 | `GET /admin/v1/sessions/{id}/quality` + `/events` |
| 会话列：平均延迟、链路、结束原因 | `SessionRow` 无字段 | `SessionRow` 增 `avg_latency_ms`、`link`、`end_reason` |

## 数据来源要求（不能造数）

- 采样必须来自真实执行点：会话建立/授权/就绪/关闭事件、媒体管线（编码/发送/接收/解码/呈现时间）、中继与 P2P 选择结果。
- 无采样时返回 `available:false` + 空数组，**禁止返回 0 曲线或样例百分比**。
- 聚合口径与客户端埋点一致（PRD §6.3.1 验收：误差 ≤1%）。

## 必须通过的断言（执行时）

- [ ] `TestQualityMetricsUnavailableWithoutSamples`：无采样时 `available=false`，不出现 0 值曲线。
- [ ] `TestSessionQualityPercentiles`：P50/P95 由真实采样计算（给定样本可复算）。
- [ ] `TestDeviceQualityDailyBuckets`：30 天按日聚合，缺失日期返回 `null` 而不是 0。
- [ ] `TestOverviewAddsUsersOnlineAndNewDevices`：两个新字段有权威来源（presence 租约 / 设备创建时间）。

## 状态

**未开始**。ADM-UI-04/06a/07a 已把入口做成诚实的「数据未提供」；接口落地后按 ADM-UI-04b/06b/07b 接真实图表。

## 进度

- 2026-11-23（ADM-API-02a）：`Overview.users_online` 已落地（按账号去重，presence 权威判定），仪表盘「在线用户」显示真值。
- `devices_new_24h` **仍缺**：`store.Device` 没有创建时间字段，需要设备表加列 + 迁移，单独排期。

## 进度（续）

- 2026-11-23（ADM-API-02c）：会话质量采样链路已落地——`POST /v1/sessions/quality`（设备上报）+ `GET /admin/v1/sessions/quality`（管理端查询），A8 展示真实采样与聚合。
- 仍缺：按日聚合的设备 30 天曲线、仪表盘质量/稳定性指标、趋势图（需要聚合与图表库）。

## 进度（续）

- 2026-11-23（ADM-API-02d）：设备质量按日聚合接口 + A6 30 天曲线已落地；顺带修掉迁移文件放错目录（未被打包）的缺陷。
- 仍缺：仪表盘质量/稳定性指标组与趋势图（需要聚合任务与图表库）。

## 进度（续）

- 2026-11-23（ADM-API-02e）：Overview 增加 `range` 窗口与质量指标（P50/P95/中继占比/采样数），仪表盘质量卡显示真值；无采样仍显示「数据未提供」。
- 仍缺：建连成功率、P2P 命中率、帧率达成率（需要建连埋点/档位目标值）、稳定性组（异常中断率/崩溃率/重连成功率）、趋势图。
