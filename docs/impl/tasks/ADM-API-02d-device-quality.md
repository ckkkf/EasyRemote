# ADM-API-02d 设备质量按日聚合 + 30 天曲线（A6）

> 来源：`docs/impl/tasks/ADM-API-02-dashboard-metrics-contract.md`（设备 30 天曲线）
> 需求：`docs/spec/pages.md` A6（行 231）、PRD §6.3.3（行 597-601「近 30 天的会话质量趋势」）
> 前置：ADM-API-02c（按秒采样入站）

## 目标

把按秒采样聚合为**按天**的延迟/丢包/中继占比/采样数，给 A6 设备详情提供真实 30 天曲线数据；没有采样时显示「没有质量采样」，不画 0 曲线。

## 输入 / 输出

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-18 `GET /admin/v1/devices/quality?device_id=&days=30` | — | `DeviceQualityResult{device_id, available, days: list<DeviceQualityDay>}` |

`DeviceQualityDay{date(YYYY-MM-DD UTC), avg_latency_ms, avg_loss_permille, relay_percent, samples}`。

## 迁移目录修复（本轮发现并修）

003/004/005 三个迁移此前被放在**仓库根目录** `migrations/`，而 `go:embed` 读的是 `server/internal/stack/migrations/` —— 也就是说这些列/表在真实 PG 上**从未生效**（内存 store 测试看不出来）。本轮把文件移到 embed 目录，并新增 `TestLoadMigrationIncludesAllFiles` 守住"文件必须被打包"。

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：46 条端点一致。
- [ ] `TestLoadMigrationIncludesAllFiles`：迁移拼接包含 accounts / session_quality_samples / nickname / device_name。
- [ ] `TestMemoryContract/AggregateDeviceQuality`：同日平均、跨天分组、窗口外为空、无关设备为空。
- [ ] `TestDeviceQualityAggregates`：无采样 `available=false`；有采样返回按日聚合；未知角色拒绝。
- [ ] `TestDevicesE2E`：真实上报两天采样 → A6 显示 2 行按日聚合（30ms/0%、40ms/100%），区间按钮可用。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/003-005*.sql`（移动）、`migration_test.go`（新增）
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`device_quality_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`features/devices/device-detail-page.tsx`
- `scripts/admin-ui-e2e-devices.py`
- `docs/impl/tasks/ADM-API-02d-device-quality.md`、`docs/impl/evidence/ADM-API-02d/**`

禁止：手改生成物；把无采样画成 0 曲线；把迁移文件放在 embed 目录之外。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/stack/ ./internal/store/ ./internal/adminapi/ -run 'LoadMigration|Contract|DeviceQuality' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-devices.py
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (21 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E；删除 `migration_test.go`、`device_quality_test.go` 与 `docs/impl/evidence/ADM-API-02d/`（迁移文件保留在 embed 目录）。
