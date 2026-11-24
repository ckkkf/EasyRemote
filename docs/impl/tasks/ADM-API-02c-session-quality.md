# ADM-API-02c 会话质量采样（入站上报 + 管理端查询）

> 来源：`docs/impl/tasks/ADM-API-02-dashboard-metrics-contract.md`（会话每秒质量曲线）
> 需求：`docs/spec/pages.md` A8（行 240-242）、PRD §6.3.4（行 607「看单次会话的质量曲线」）
> 前置：ADM-API-02a（Overview.users_online）、ADM-UI-07a（A8 页面）

## 目标

建立**真实**的质量数据链路：客户端按秒上报采样 → 服务端只追加存储 → 管理端只读展示聚合与采样表。没有采样时明确「没有采样数据」，不画 0 曲线。

## 输入 / 输出

| 端点 | 请求 | 响应 | 鉴权 |
|---|---|---|---|
| GW-26 `POST /v1/sessions/quality` | `QualityReportRequest{session_id, samples: list<QualitySample>}` | `OkResult` | device（必须是会话双方之一） |
| ADM-17 `GET /admin/v1/sessions/quality?session_id=` | — | `SessionQualityResult{session_id, available, samples}` | admin（session.read） |

`QualitySample{ts, latency_ms, loss_permille, fps, tier, relay}`；同一会话同一秒幂等覆盖。

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：45 条端点一致。
- [ ] `TestMemoryContract/QualitySamples`：同秒覆盖不重复、按时间排序、无采样会话返回空。
- [ ] `TestSessionQualityAvailableFlag`：无采样 `available=false`；有采样返回真实值；未知角色 `ADMIN_FORBIDDEN`。
- [ ] `TestSessionsE2E`：用真实设备凭证上报 3 条采样 → A8 页面渲染 3 行 + 平均延迟/中继占比；无采样会话显示「该会话没有采样数据」。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`（两端）
- `migrations/005_session_quality.sql`
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/gateway/handler.go`
- `server/internal/adminapi/adminapi.go`、`quality_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`features/sessions/session-detail-page.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-02c-session-quality.md`、`docs/impl/evidence/ADM-API-02c/**`

禁止：手改生成物；用前端计时器/随机数伪造采样；把 `available=false` 画成 0 曲线。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ -run 'Contract|SessionQuality' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (21 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/route_specs/store/gateway/adminapi/handler/前端/E2E；删除 005 迁移与 `docs/impl/evidence/ADM-API-02c/`。
