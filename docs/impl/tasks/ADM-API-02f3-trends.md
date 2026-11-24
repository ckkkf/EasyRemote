# ADM-API-02f3 时间桶趋势 + 仪表盘趋势卡

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（拆分子卡 3）
> 需求：`docs/spec/pages.md` A2（行 208「24h / 7d / 30d 趋势」）、PRD §6.3.1（行 583）
> 前置：ADM-API-02e/02f1/02f2

## 目标

把"趋势"从「接口未提供」变成真实曲线：

1. `GET /admin/v1/stats/trends?range=24h|7d|30d&bucket=hour|day` 按时间桶聚合 `sessions_started / connect_attempts / connect_active`；
2. 仪表盘「趋势」卡用 SVG polyline 画三条线（不引入图表库，避免新增依赖；后续需要交互再引 Recharts）；
3. 空窗口显示「窗口内没有可统计的事件」，不画空曲线。

## 输入 / 输出

| 端点 | 参数 | 响应 |
|---|---|---|
| ADM-34 `GET /admin/v1/stats/trends` | `range`、`bucket` | `TrendsResult{range, bucket, points:[{bucket_start, sessions_started, connect_attempts, connect_active}]}` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：62 条端点一致。
- [ ] `TestTrendsBuckets`：两小时两个桶、会话与建连分别计数、非法 range/bucket `ADMIN_CONFIG_INVALID`、无权限 `ADMIN_FORBIDDEN`。
- [ ] `TestTrendPathMapsBuckets`：空序列返回空串；3 点折线峰值 y 最小、0 值贴近底部。
- [ ] `TestDashboardE2E`：趋势卡显示「1 个时间桶」、polyline 有点、显示峰值。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/adminapi/{adminapi,trends_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`App.tsx`、`tests/{dashboard-metrics,admin-scope}.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02f3-trends.md`、`docs/impl/evidence/ADM-API-02f3/**`

禁止：前端伪造时间桶；空窗口画 0 线；用 5s 轮询次数冒充事件数。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'TrendsBuckets' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (23 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 `trends_test.go` 与 `docs/impl/evidence/ADM-API-02f3/`。
