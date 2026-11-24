# ADM-API-03g 会话详情：双方设备 / 链路切换 / 状态时间线

> 来源：`docs/impl/tasks/ADM-API-03f2-link-abnormal-filter.md` 之后（A8 剩余）
> 需求：`docs/spec/pages.md` A8（行 240-242）
> 前置：ADM-API-02c（采样）、02f4（链路上报）、02f7（会话摘要）

## 目标

`GET /admin/v1/sessions/detail?session_id=` 返回：

1. `controller/target`：双方设备摘要（设备码/平台/系统/客户端/在线）；
2. `link_switches`：按采样时间顺序检测 link 变化（p2p↔relay），只记录真实上报的变化；
3. `timeline`：状态时间线（创建 / 结束 + 结束原因）。

**错误码时间线不做**：服务端没有按会话记录错误事件；不臆造。A8 因此保持 partial。

## 必须通过的断言

- [ ] `gen-contract --check` / `check-routes --strict`：70 条端点一致。
- [ ] `TestSessionDetailPartiesAndSwitches`：双方设备字段、p2p→relay 切换 1 条、时间线含结束原因。
- [ ] `TestSessionsE2E`：详情页双方设备 2 行、链路切换 1 行（p2p→relay）、时间线 ≥1 行。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/adminapi/{adminapi,session_detail_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`features/sessions/session-detail-page.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-03g-session-detail.md`、`docs/impl/evidence/ADM-API-03g/**`

禁止：伪造错误码时间线；把"没有切换"显示成 0 条假记录。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'TestSessionDetailPartiesAndSwitches' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (36 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E；删除 `session_detail_test.go` 与 `docs/impl/evidence/ADM-API-03g/`。
