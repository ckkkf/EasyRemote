# ADM-API-04 设备模块契约补齐（字段 / 筛选 / 操作 / 质量聚合）

> 来源：ADM-UI-06a 执行时确认的缺口；`docs/spec/pages.md` A5（行 223-227）、A6（行 229-232）、PRD §6.3.3（行 594-601）
> 性质：**contract-first 后端卡**。先改契约、再生成、再实现。

## 缺口（当前 DeviceRow{id, device_code, account_id, platform, online, last_seen}）

| 需求 | 现状 | 需要的契约变化 |
|---|---|---|
| 列：设备名、系统版本、客户端版本、NAT 类型 | 无字段 | `DeviceRow` 增 `device_name`、`os_version`、`client_version`、`nat_type` |
| 筛选：设备码/设备名/账号/平台/版本/在线状态 | 只有 `q` + `platform` | `GET /admin/v1/devices` 增 `online`、`client_version`、`device_name`（服务端过滤，presence 走权威租约） |
| 操作：强制下线、解绑、标记风险 | 无接口 | `POST /admin/v1/devices/force-offline`、`/unbind`、`/flag-risk`（均需原因 + 审计） |
| A6 近 30 天质量曲线（延迟/丢包/档位/中继） | 无接口 | `GET /admin/v1/devices/{id}/quality?range=30d`（依赖 ADM-API-02 的采样模型） |

## 必须通过的断言（执行时）

- [ ] `TestAdminEnvelopeContract` 覆盖新路由；`check-routes --strict` 绿。
- [ ] `TestListDevices_OnlineFilterUsesPresence`：`online=true` 只返回权威 presence 在线设备，不能读设备表历史布尔值。
- [ ] `TestDeviceForceOfflineRequiresReason` / `TestDeviceForceOfflineWritesAudit`。
- [ ] `TestDeviceQualityReturnsEmptyForNoSamples`：无采样时返回空数组 + `available:false`，不能返回 0 曲线。

## 状态

**未开始**。ADM-UI-06a 已把入口做成"禁用 + 说明"，不阻塞 UI 整改。

## 进度

- 2026-11-23（ADM-API-04a）：`device_name`/`os_version`/`client_version`/`nat_type` 已落地（迁移 004 + bind 可选上报 + 管理端展示）。
- 仍缺：在线状态筛选、强制下线/解绑/标记风险、30 天质量曲线（依赖 ADM-API-02 采样）。

## 进度（续）

- 2026-11-23（ADM-API-04b）：在线状态筛选（`online=1/0`）、强制下线（关会话+离线+审计）、解绑（删除+审计，仅 super）已落地。
- 仍缺：标记风险设备（需风险列与解除流程，ADM-API-04c）、30 天质量曲线（ADM-API-02）。

## 进度（续）

- 2026-11-23（ADM-API-04c）：设备风险标记/解除已落地（迁移 006 + 两条端点 + 列表徽标 + 审计）。
- 仍缺：30 天曲线已由 ADM-API-02d 提供；自动处置（黑名单联动）属 A10。
