# ADM-API-02f1 会话结束原因归一 + 异常中断率

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（拆分子卡 1）
> 需求：`docs/spec/pages.md` A2（行 204-208）、PRD §6.3.1（行 577-583）
> 前置：ADM-API-02e（Overview 质量指标与统计窗口）

## 目标

把"会话异常中断率"从"无数据源"变成可计算指标：

1. `session.ClassifyClose(reason)` 把自由文本结束原因归一为 `user_hangup / admin_kill / timeout / peer_offline / network_lost / unknown`；
2. 异常 = `{timeout, peer_offline, network_lost, unknown}`（用户主动挂断与后台人工中断不算异常）；
3. Overview 增加 `stability_available / closed_sessions / abnormal_closes / abnormal_close_rate`，按统计窗口计算；
4. 仪表盘稳定性组显示真实异常中断率；崩溃率/重连成功率仍显示「数据未提供」。

## 口径（唯一来源：`session/closekind.go`）

| 匹配 | 分类 |
|---|---|
| 空串 | unknown（算异常） |
| 前缀 `admin` | admin_kill（不算异常） |
| 含 timeout / timed out / expired | timeout |
| 含 network / connection lost / udp | network_lost |
| 含 offline / peer closed / disconnect | peer_offline |
| 含 hangup / hang up / user close | user_hangup（不算异常） |
| 其它 | unknown |

匹配顺序从具体到宽泛，避免 `admin force-offline` 被误判成 offline。

## 必须通过的断言

- [ ] `TestClassifyCloseKinds`：13 组输入全部归类正确。
- [ ] `TestIsAbnormalExcludesUserAndAdmin`：用户挂断/后台中断不算异常；timeout/network/peer/空 算异常。
- [ ] `TestOverviewStabilityMetrics`：无结束会话时 `stability_available=false`；1 正常 + 1 异常 → closed=2、abnormal=1、rate=50。
- [ ] `TestDashboardE2E`：真实关闭一条 reason=`network changed` 的会话 → 页面显示 100% 且副标题显示结束会话数；崩溃率/重连成功率仍「数据未提供」。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/session/closekind.go`、`closekind_test.go`（新增）
- `server/internal/adminapi/adminapi.go`、`stability_test.go`（新增）
- `web-admin/src/api.ts`、`features/dashboard/metrics.ts`、`tests/dashboard-metrics.test.ts`
- `scripts/admin-ui-e2e-dashboard.py`
- `docs/impl/tasks/ADM-API-02f1-close-kinds.md`、`docs/impl/evidence/ADM-API-02f1/**`

禁止：把"用户挂断"算异常；用 0/0 显示 0%；在客户端计时器上算比率。

## 验收命令

```bash
cd server && go test ./internal/session/ ./internal/adminapi/ -run 'ClassifyClose|IsAbnormal|OverviewStability' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-dashboard.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (19 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/adminapi/前端/E2E；删除 `closekind.go`、`closekind_test.go`、`stability_test.go` 与 `docs/impl/evidence/ADM-API-02f1/`。
