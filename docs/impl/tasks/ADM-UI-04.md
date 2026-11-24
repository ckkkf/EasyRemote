# ADM-UI-04 仪表盘：指标分组、5s 刷新与缺数据诚实态

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-04（行 137-143）
> 需求：PRD §6.3.1（行 570-583）、`docs/spec/pages.md` A2（行 201-208）
> 前置：ADM-UI-01a（primitives）、02a（路由）、02b（三态）、02c（对话框）、03（登录）

## 目标

把仪表盘按 PRD 的三组指标（规模/质量/稳定性）重构，落实 5s 刷新 + 可暂停 + 最后更新时间 + 陈旧标记 + 失败保留旧数据；**没有接口的指标一律显示「数据未提供」**，不用 0、样例百分比或假曲线填充；同时修掉「在线率用分页样本当总数」的口径错误。

## 输入 / 输出

不改 API、不改契约，字段映射：

| 展示项 | 来源 |
|---|---|
| 在线设备 / 设备总数 / 进行中会话 / 待授权会话 | `Overview.devices_online/devices_total/sessions_active/sessions_pending`（`contract.yaml` `Overview`） |
| 在线用户 / 24h 新增设备 / 质量组 6 项 / 稳定性组 3 项 | **契约无字段** → 渲染「数据未提供」，缺口卡 ADM-API-02 |
| 趋势 24h/7d/30d、平台/版本下钻 | **接口未提供** → 文案说明，不渲染曲线 |
| 最近会话 / 最近审计 / 平台分布 | `GET /admin/v1/sessions`、`/audit`、`/devices`（平台分布明确标注为本页样本） |

## 必须通过的断言

- [ ] `TestMetricGroupsOnlyUseAuthoritativeFields`：规模组 4 项取 overview 真值，2 项为 null；质量组 6 项、稳定性组 3 项全部 null 且 hint 为「数据未提供」。
- [ ] `TestUnavailableMetricsAreNotZero`：任何 null 指标都不能渲染成 `0`；overview 为 null 时不得编数字。
- [ ] `TestPollIntervalMatchesPrd`：`POLL_INTERVAL_MS=5000`、`STALE_AFTER_MS=15000`。
- [ ] `TestStaleThreshold`：14.999s 不算陈旧、15s 起算陈旧、从未更新算陈旧。
- [ ] `TestOnlineRateUsesAuthoritativeTotal`：在线率用 `devices_online/devices_total`，不能用分页样本；total=0 时返回 null。
- [ ] `TestDashboardE2E`（`python3 scripts/admin-ui-e2e-dashboard.py`）：12 条全 PASS —— 三组渲染、规模取真值、质量/稳定性 9 项全「数据未提供」、趋势文案、更新于、5s 内再次请求、暂停后不再请求、暂停 15s 出陈旧标记、恢复后重新请求且标记消失、无 pageerror。
- [ ] 既有回归全绿：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 视觉基线变更（有意变更，必须记录）

与 ADM-UI-00 基线相比，**只有 a2 变化**，其余 5 屏 DOM 判据完全一致：

```text
a2-dashboard.metricCards: 4 -> 15
a2-dashboard.metricLabels: [设备总数,在线设备,进行中会话,待授权] ->
  [在线设备,设备总数,进行中会话,待授权会话,在线用户,24h 新增设备,
   建连成功率,P2P 命中率,中继占比,延迟 P50,延迟 P95,帧率达成率,
   会话异常中断率,客户端崩溃率,重连成功率]
```

新的比对基线为 `docs/impl/evidence/ADM-UI-04/screens/manifest.json`；ADM-UI-00 的 manifest 保留为历史基线，不覆盖。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-04.md`、`docs/impl/evidence/ADM-UI-04/**`
- `web-admin/src/features/dashboard/metrics.ts`（新增：指标口径与刷新常量）
- `web-admin/src/App.tsx`（**仅** Dashboard 组件与对应 import）
- `web-admin/tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/evidence/ADM-UI-00/page-matrix.md`（仅追加变更记录）

禁止：改其它页面、改 `docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止给缺失指标填 0/样例值；禁止改 ADM-UI-00 的 manifest 或放宽比对。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-dashboard.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-04/screens --compare-to docs/impl/evidence/ADM-UI-00/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (12 checks)`；基线差异只有 a2 且已在证据里逐项列出；gate `GATE: PASS`。

## 回滚

删除 `web-admin/src/features/dashboard/metrics.ts`、`web-admin/tests/dashboard-metrics.test.ts`、`scripts/admin-ui-e2e-dashboard.py`、`docs/impl/evidence/ADM-UI-04/`；`git checkout -- web-admin/src/App.tsx`。
