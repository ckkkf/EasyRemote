# ADM-API-03f1 会话筛选下推（时间范围 / 账号 / 设备）

> 来源：`docs/impl/tasks/ADM-API-03f-filters.md`（拆分子卡 1）
> 需求：`docs/spec/pages.md` A7（行 234-238）
> 前置：ADM-API-02f7（会话质量摘要）

## 目标

把会话筛选下推到存储层（List 与 Count 共用同一 where 构造，分页总数与列表一致）：

1. 时间范围：`from`/`to` 作用于 `started_at`；
2. 账号：按 controller/target 设备的 `account_id` 关联；
3. 设备：controller 或 target 命中即可。

## 必须通过的断言

- [ ] `TestSessionFilters`：时间范围只返回窗口内会话；账号过滤只返回该账号设备参与的会话；设备过滤同理；组合筛选不误包含。
- [ ] `TestSessionsE2E`：未来时间范围 0 行（空态文案）、清空恢复；设备筛选后所有行都包含该设备 ID。
- [ ] 六屏判据不变（新增输入不影响 a7 列判据）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `server/internal/store/{store,memory,pg}.go`
- `server/internal/adminapi/{adminapi,session_filters_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-API-03f1-session-filters.md`、`docs/impl/evidence/ADM-API-03f1/**`

禁止：前端过滤冒充服务端筛选；Count 与 List 条件不一致。

## 验收命令

```bash
cd server && go test ./internal/adminapi/ -run 'TestSessionFilters' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (31 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` store/adminapi/handler/前端/E2E；删除 `session_filters_test.go` 与 `docs/impl/evidence/ADM-API-03f1/`。
