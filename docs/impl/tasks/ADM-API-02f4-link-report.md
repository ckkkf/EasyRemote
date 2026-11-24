# ADM-API-02f4 客户端链路上报（P2P 命中率 + 帧率达成率）

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（剩余项）
> 需求：`docs/spec/pages.md` A2（行 204-208）、PRD §6.3.1（行 577-583）
> 前置：ADM-API-02c（采样入站）、02e/02f1/02f2/02f3

## 目标

把 A2 里最后两个"可计算但缺数据"的指标补上——数据来自**客户端上报**，服务端不猜：

1. `QualitySample` 增 `link`（p2p / relay / 空=未上报）与 `target_fps`（0=未上报）；
2. 网关只接受合法值：`link` 非 p2p/relay 一律按未上报存空；`target_fps` 限 0-240；
3. Overview 增 `link_available / p2p_hit_rate / fps_available / fps_target_rate`：
   - P2P 命中率 = p2p / (p2p + relay)，只统计上报过 link 的采样；
   - 帧率达成率 = fps ≥ target_fps 的采样 / 上报过 target_fps 的采样。

## 必须通过的断言

- [ ] `gen-contract --check`：三端一致（68 类型 / 68 接口）。
- [ ] `TestOverviewLinkAndFpsRates`：无上报时两个 available=false；2 p2p + 2 relay → 50%；3 条带目标帧率、2 条达成 → 67%。
- [ ] `TestDashboardE2E`：真实上报 1 p2p + 2 relay、目标 60fps（达成 1 条）→ 页面显示 P2P 33%、帧率达成 33%。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/stack/migrations/013_quality_link.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/gateway/handler.go`
- `server/internal/adminapi/{adminapi,link_stats_test}.go`
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02f4-link-report.md`、`docs/impl/evidence/ADM-API-02f4/**`

禁止：用"没签中继票据"反推 P2P；把未上报当 0%；对非法 link 值静默当 p2p。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/adminapi/ -run 'OverviewLinkAndFpsRates' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (24 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 013 迁移、`link_stats_test.go` 与 `docs/impl/evidence/ADM-API-02f4/`。
