# ADM-UI-07b2 双人确认中断页面

> 来源：`docs/impl/tasks/ADM-UI-07b-kill-double-confirm.md`（拆分子卡 2）
> 需求：`docs/spec/pages.md` A9（行 244-246）、PRD §6.3.4（行 606）
> 前置：ADM-API-07b1（发起/确认/过期接口）

## 目标

给双人确认中断提供真实入口：

1. 会话列表每行新增「双人中断」按钮 → 填原因提交请求；
2. 列表下方出现「待确认中断」卡片（会话/发起人/原因/过期时间/确认按钮）；
3. 发起人自己点确认 → 服务端拒绝（`ADMIN_FORBIDDEN`），页面显示错误态；
4. 第二位管理员确认后会话关闭、卡片消失、写 `session.kill_confirm` 审计。

## 输入 / 输出

| 交互 | 接口 |
|---|---|
| 发起 | `POST /admin/v1/sessions/kill-request {session_id, reason}` |
| 待确认列表 | `GET /admin/v1/sessions/kill-requests?status=pending` |
| 确认 | `POST /admin/v1/sessions/kill-confirm {request_id}` |

## 必须通过的断言

- [ ] `npm test -- --run`：路由与既有断言不变（56 tests）。
- [ ] `TestSessionsE2E`：提交请求需原因；待确认卡片出现且写 `session.kill_request` 审计；发起人确认被拒（`ADMIN_FORBIDDEN`）；`ops2-dev` 确认后会话 closed 且卡片消失；写 `session.kill_confirm` 审计。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `web-admin/src/api.ts`、`App.tsx`
- `server/internal/stack/{stack_pg.go,seed_test.go}`（开发环境 seed 可登录的第二操作人 `ops2-dev`）
- `scripts/admin-ui-e2e-sessions.py`
- `docs/impl/tasks/ADM-UI-07b2-kill-double-confirm-ui.md`、`docs/impl/evidence/ADM-UI-07b2/**`
- `docs/impl/evidence/ADM-UI-00/page-matrix.md`（A9 描述）

禁止：前端本地标记"已确认"；绕过服务端同人校验；给无权限角色显示入口。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-sessions.py
bash scripts/gate.sh
```

## 完成标准

三条断言全绿；E2E `E2E OK (28 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` api/App/stack seed/E2E；删除 `docs/impl/evidence/ADM-UI-07b2/`；矩阵 A9 描述回退。
