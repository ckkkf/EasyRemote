# ADM-API-03f2 会话筛选：链路类型 + 异常结束

> 来源：`docs/impl/tasks/ADM-API-03f2-link-abnormal-filter.md`
> 需求：`docs/spec/pages.md` A7（行 234-238）
> 前置：ADM-API-02f1（分类规则）、02f7（会话摘要）、03f1（筛选下推）

## 目标

1. `sessions.close_kind` 落库：关闭时按 `session.ClassifyClose` 写分类（迁移 016 + PG 列/scan/insert/update）；
2. 列表支持 `close_kind`、`abnormal=1|0`、`link=p2p|relay|mixed` 筛选，List/Count 共用 `sessionWhere`；
3. "异常结束"只对 `state='closed'` 生效（未结束会话没有"结束类型"）；
4. 前端新增「链路筛选」「结束类型筛选」两个下拉。

## 必须通过的断言

- [ ] `TestSessionCloseKindAndFilters`：关闭后 `close_kind=user_hangup`；abnormal=true 排除正常结束、包含异常结束；abnormal=false 包含正常结束；link=relay 只返回该会话。
- [ ] `TestSessionsE2E`：链路筛选后所有行都是「中继」；异常结束筛选只返回已结束且非 admin 中断的会话。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `server/internal/stack/migrations/016_session_close_kind.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/session/session.go`
- `server/internal/adminapi/session_closekind_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-03f2-link-abnormal.md`、`docs/impl/evidence/ADM-API-03f2/**`

禁止：SQL 里另写一套分类规则；未结束会话参与"异常结束"筛选；前端过滤冒充服务端。

## 验收命令

```bash
cd server && go test ./internal/adminapi/ -run 'TestSessionCloseKindAndFilters' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

三条断言全绿；E2E `E2E OK (33 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` store/session/adminapi/handler/前端/E2E；删除 016 迁移、`session_closekind_test.go` 与 `docs/impl/evidence/ADM-API-03f2/`。
