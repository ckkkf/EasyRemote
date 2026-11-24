# ADM-UI-06a 设备列表 DataTable + 详情独立路由

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-06（行 154-160）
> 需求：`docs/spec/pages.md` A5（行 223-227）、A6（行 229-232）、PRD §6.3.3（行 594-601）
> 前置：ADM-UI-05a（DataTable/详情路由范式）、ADM-UI-02a/02b/02c
> 拆分说明：设备名/系统版本/客户端版本/NAT、在线状态筛选、强制下线/解绑/风险、30 天曲线都依赖 ADM-API-04 / ADM-API-02。本卡只做不依赖新接口的部分。

## 目标

1. 设备列表改用 DataTable（列与分页不变），行点击进入 `/devices/:id`；
2. 设备详情从 modal 改为独立路由：设备信息 + **服务端按设备过滤**的最近会话；
3. 近 30 天质量曲线明确「telemetry 接口未提供」，时间段按钮禁用；强制下线/更多筛选/解绑等无接口能力禁用并注明原因。

## 输入 / 输出

不改 API、不改契约。字段映射：

| 展示项 | 来源 |
|---|---|
| 设备码 / 归属账号 / 平台 / 在线 / 最后在线 | `DeviceRow` |
| 最近会话 | `GET /admin/v1/sessions?q=<device_id>`（store 的 q 覆盖 controller/target id，服务端过滤） |
| 设备名 / 系统版本 / 客户端版本 / NAT | 契约无字段 → 「接口未提供（ADM-API-04）」 |
| 30 天质量曲线 | 无 telemetry → 「接口未提供（ADM-API-02）」，不画曲线 |
| 强制下线 / 解绑 / 标记风险 / 在线筛选 | 无接口 → 禁用 + title 说明 |

## 必须通过的断言

- [ ] `TestDevicesE2E`（`python3 scripts/admin-ui-e2e-devices.py`）：12 条全 PASS —— 列、更多筛选禁用、行内更多禁用、复制设备码不跳转、行点击进 `/devices/<id>`、按设备过滤的会话、曲线未提供、时间段按钮禁用、强制下线禁用、深链刷新、返回列表、未知设备「设备不存在」、无 pageerror。
- [ ] `TestVisualBaselineUnchanged`：与 ADM-UI-05 基线相比只有 a6 的**有意**变化（`modalTitle` 去掉「设备详情」前缀、`kvPairs` 6→7 因新增"设备 ID"），其余 5 屏判据一致。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-06a.md`、`docs/impl/tasks/ADM-API-04-device-contract.md`、`docs/impl/evidence/ADM-UI-06/**`
- `web-admin/src/features/devices/device-detail-page.tsx`（新增）
- `web-admin/src/App.tsx`（Devices 改 DataTable、删除 DeviceSessions/DeviceDetail、新增 `:id` 路由）
- `web-admin/tests/dialog-usage.test.ts`（详情迁路由后仍校验 DetailDialog 组件本身）
- `scripts/admin-ui-e2e-devices.py`
- `scripts/admin-ui-baseline.py`（a6 改量独立路由页面）

禁止：改 `docs/spec/**`、`server/**`、`prototype/**`；禁止用前端过滤冒充服务端按设备查询；禁止画假曲线。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-devices.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-06/screens --compare-to docs/impl/evidence/ADM-UI-05/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

两条断言全绿；E2E `E2E OK (12 checks)`；视觉差异只有 a6 且逐项列出；gate `GATE: PASS`。

## 回滚

删除 `device-detail-page.tsx`、`scripts/admin-ui-e2e-devices.py`、`docs/impl/evidence/ADM-UI-06/`；`git checkout -- web-admin/src/App.tsx web-admin/tests/dialog-usage.test.ts scripts/admin-ui-baseline.py`。
