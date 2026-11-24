# ADM-API-02f2 建连埋点 + 建连成功率

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（拆分子卡 2）
> 需求：`docs/spec/pages.md` A2（行 204-208）、PRD §6.3.1（行 577-583）
> 前置：ADM-API-02e（Overview 质量指标）、ADM-API-02f1（结束原因归一）

## 目标

把"建连成功率"变成可计算指标：每次 `/v1/connect/request` 成功创建会话记一次尝试，进入 `active` 记成功。

**P2P 命中率仍不做**：服务端只知道"是否签发过中继票据"，无法反推 P2P（需要客户端上报链路类型）。不用推导值冒充。

## 数据链路

| 事件 | 写入点 | 结果 |
|---|---|---|
| 发起连接 | `/v1/connect/request` 创建会话后 | `connect_attempts` 一行，outcome=requested |
| 双方 ready 进入 active | `/v1/sessions/ready` 返回 active | outcome=active |
| 签发中继票据 | `/v1/relay/ticket` | link=relay（p2p 不猜） |

## 字段

`Overview` 新增 `connect_available / connect_attempts / connect_success_rate`（窗口内 active 尝试 / 总尝试，四舍五入）。

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：61 条端点一致。
- [ ] `TestOverviewConnectRate`：无尝试时 `connect_available=false`；2 次尝试 1 次 active → 50%。
- [ ] `TestConnectAttemptMarksActive`：真实 connect→consent→ready 双端后统计 total=1、active=1。
- [ ] `TestDashboardE2E`：页面显示建连成功率 0%（1 次尝试、0 次 active）+ 「尝试 1 次」；P2P 命中率仍「数据未提供」。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/stack/migrations/011_connect_attempts.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/gateway/handler.go`、`connect_attempt_test.go`（新增）
- `server/internal/adminapi/adminapi.go`、`connect_stats_test.go`（新增）
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02f2-connect-attempts.md`、`docs/impl/evidence/ADM-API-02f2/**`

禁止：把"没签中继票据"当成 P2P；用 0/0 显示 0%；埋点失败静默吞掉。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ ./internal/gateway/ -run 'OverviewConnectRate|ConnectAttemptMarksActive' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (21 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 011 迁移、两个新测试与 `docs/impl/evidence/ADM-API-02f2/`。
