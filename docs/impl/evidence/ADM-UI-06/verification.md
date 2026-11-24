# ADM-UI-06a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-06a.md`
> 时间：2026-11-23 11:24-11:34（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/features/devices/device-detail-page.tsx` | A6 独立路由详情：设备信息 + 服务端按设备过滤的最近会话 + 曲线/操作未开放说明 |
| `web-admin/src/App.tsx` | Devices 改 DataTable、行点击路由跳转、新增 `:id` 路由、删除 DeviceSessions/DeviceDetail modal、更多筛选/更多操作禁用 |
| `web-admin/tests/dialog-usage.test.ts` | 详情迁路由后仍校验 `DetailDialog` 组件本身（保留给 A8 复用） |
| `scripts/admin-ui-e2e-devices.py` | 12 条真实 E2E |
| `scripts/admin-ui-baseline.py` | a6 改量独立路由页面（不再找 `[role=dialog]`） |
| `docs/impl/tasks/ADM-API-04-device-contract.md` | 设备字段/筛选/操作/质量聚合的 contract-first 卡 |

## 2. 字段映射

| 展示项 | 来源 |
|---|---|
| 设备码/归属账号/平台/在线/最后在线 | `DeviceRow` |
| 最近会话 | `GET /admin/v1/sessions?q=<device_id>`（服务端过滤 controller/target id） |
| 设备名/系统版本/客户端版本/NAT | 契约无字段 → 「接口未提供（ADM-API-04）」 |
| 30 天质量曲线 | 无 telemetry → 「接口未提供（ADM-API-02）」 |
| 强制下线/解绑/风险/在线筛选 | 无接口 → 禁用 + title |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 列表/详情/会话过滤/未开放/深链/未知设备 | `TestDevicesE2E` | PASS 12/12 |
| 六屏视觉判据 | `TestVisualBaselineUnchanged` | 仅 a6 有意变化（见下） |
| 回归 / 技术栈 / 闸门 | `npm test`（32）/ `stack-check` 4/4 / `gate.sh` | 全绿 |

## 4. 关键输出

```text
Tests  32 passed (32)
PASS row opens device detail -> http://127.0.0.1:5399/devices/dev-635eafbad4c45d48
PASS device sessions filtered by device -> 1
PASS 30-day curve marked unavailable
PASS force offline disabled
E2E OK (12 checks)
GATE: PASS
```

视觉差异（有意）：

```text
a6-device-detail.kvPairs: 6 -> 7            # 新增"设备 ID"一行
a6-device-detail.modalTitle: '设备详情 ### ### ###' -> '### ### ###'   # modal 改独立路由页面
```

其余 5 屏判据与 ADM-UI-05 基线一致。

## 5. 未完成项、限制与风险

- **30 天质量曲线仍无数据源**：需要 telemetry 采样模型（ADM-API-02）+ 设备质量聚合接口（ADM-API-04）。本卡明确「接口未提供」，没有画假曲线——客服排查靠假曲线会误导判断。
- 设备名/系统版本/客户端版本/NAT、在线状态筛选、强制下线/解绑/标记风险均依赖 ADM-API-04；按钮禁用并写明原因。
- "最近会话"是服务端按设备过滤的真实数据，但没有质量字段（延迟/链路/结束原因），这些属于 ADM-API-02/ADM-UI-07。
- 设备详情页尚未做 1024/768 响应式检查（属 ADM-UI-10）。

## 6. 回滚

删除 `device-detail-page.tsx`、`scripts/admin-ui-e2e-devices.py`、`docs/impl/evidence/ADM-UI-06/`；`git checkout -- web-admin/src/App.tsx web-admin/tests/dialog-usage.test.ts scripts/admin-ui-baseline.py`。
