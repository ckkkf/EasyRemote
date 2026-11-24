# ADM-API-02f5 / 02f6 客户端健康上报 + 24h 新增设备

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（剩余项）
> 需求：`docs/spec/pages.md` A2（行 204-208）、PRD §6.3.1（行 577-583）
> 前置：02c/02e/02f1-02f4

## 目标

把 A2 最后三个「数据未提供」变成真实值：

1. `POST /v1/sessions/health`（GW-27，device 鉴权、会话双方校验）上报 `crash / reconnect_attempt / reconnect_success`；
2. Overview 增 `crash_available / crash_rate / reconnect_available / reconnect_success_rate`：
   - 崩溃率 = 崩溃事件 / 窗口内开始的会话数 ×100；
   - 重连成功率 = 成功重连 / 重连尝试；
3. 设备表增 `created_at`（迁移 015），Overview 增 `devices_new_available / devices_new_24h`；
4. 前端三个指标全部改为真值（无上报时仍「数据未提供」）。

## 必须通过的断言

- [ ] `gen-contract --check` / `check-routes --strict`：69 条端点一致。
- [ ] `TestOverviewHealthMetrics`：无事件时两个 available=false；1 崩溃 + 1 会话 → 100%；2 尝试 1 成功 → 50%。
- [ ] `TestDashboardE2E`：真实上报后页面显示崩溃率 100%、重连成功率 50%、24h 新增设备 4。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、两端 `route_specs.go`
- `server/internal/stack/migrations/{014_health_events,015_device_created_at}.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/gateway/handler.go`
- `server/internal/adminapi/{adminapi,health_stats_test}.go`
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02f5-health-report.md`、`docs/impl/evidence/ADM-API-02f5/**`

禁止：把未上报当 0%；崩溃率分母用"全部历史会话"；24h 新增设备用前端时间过滤。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'OverviewHealthMetrics' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (25 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 014/015 迁移、`health_stats_test.go` 与 `docs/impl/evidence/ADM-API-02f5/`。
