# ADM-API-02f7 会话行质量摘要（延迟 / 链路）

> 来源：`docs/impl/tasks/ADM-API-02f-stability-trends.md`（A7/A8 剩余列）
> 需求：`docs/spec/pages.md` A7（行 234-238）、A8（行 240-242）、PRD §6.3.4（行 603-607）
> 前置：ADM-API-02c（采样）、02f4（链路上报）

## 目标

`SessionRow` 增 `latency_available / avg_latency_ms / link`，数据来自该会话的质量采样：

- 平均延迟 = 采样延迟均值；
- 主导链路 = p2p/relay 计数比较（相等记 mixed，未上报为空）；
- 无采样时 `latency_available=false`，前端显示「—」而不是 0。

批量查询（`SessionSummaries(ids)`）避免 N+1。

## 必须通过的断言

- [ ] `gen-contract --check`：三端一致（68 类型 / 69 接口）。
- [ ] `TestSessionRowsCarryQualitySummary`：3 条采样（2 p2p + 1 relay）→ avg=40、link=p2p、latency_available=true。
- [ ] `TestSessionsE2E`：列头含「平均延迟 / 链路」；有采样会话显示「xx ms」与「中继」。
- [ ] 六屏判据：仅 `a7-sessions.columns` 增加两列（预期），新 manifest 为基线。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/adminapi/{adminapi,session_summary_test}.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-02f7-session-summary.md`、`docs/impl/evidence/ADM-API-02f7/**`

禁止：前端自己算平均（必须服务端聚合）；无采样显示 0 ms。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/adminapi/ -run 'SessionRowsCarryQualitySummary' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (29 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/前端/E2E；删除 `session_summary_test.go` 与 `docs/impl/evidence/ADM-API-02f7/`。
