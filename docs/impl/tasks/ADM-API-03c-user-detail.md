# ADM-API-03c 用户详情：会话 / 操作记录 / 备注

> 来源：`docs/impl/tasks/ADM-API-03-users-contract.md`（A4 用户详情剩余缺口）
> 需求：`docs/spec/pages.md` A4（行 219-221）、PRD §6.3.2（行 588-589）
> 前置：ADM-API-03a/03b（昵称/设备数/最近登录、设备元信息）

## 目标

把用户详情页的「最近会话 / 操作记录 / 备注」从「接口未开放」变成真实数据：

1. `GET /admin/v1/users/sessions?account_id=` —— 服务端按账号名下设备过滤会话；
2. `GET/POST /admin/v1/users/note` —— 账号备注读写，写操作进审计（`user.note`，before/after）；
3. 操作记录复用 `GET /admin/v1/audit?object=<account_id>`（不新增接口）；
4. 新增权限动作 `user.write`（super/ops/cs/risk），只读角色不能写备注。

## 输入 / 输出

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-35 `GET /admin/v1/users/sessions` | `account_id`、`limit` | `list<SessionRow>` |
| ADM-36 `GET /admin/v1/users/note` | `account_id` | `AccountNoteResult`（没写过返回空串） |
| ADM-37 `POST /admin/v1/users/note` | `{account_id, note}` | `OkResult` |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：65 条端点一致。
- [ ] `TestUserDetailSessionsAndNote`：acc1 两条会话（含作为被控）、acc2 一条；备注默认空、写入后去空格可读回、写 `user.note` 审计；readonly 被拒。
- [ ] `TestUsersE2E`：详情页显示 1 条最近会话（含结束原因）、保存备注成功并写审计、刷新后备注仍在。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/012_account_notes.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/adminapi/{adminapi,user_detail_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`features/users/user-detail-page.tsx`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-users.py`
- `docs/impl/tasks/ADM-API-03c-user-detail.md`、`docs/impl/evidence/ADM-API-03c/**`

禁止：前端本地过滤会话冒充服务端查询；备注不写审计；给只读角色可写入口。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/adminapi/ -run 'UserDetailSessionsAndNote' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-users.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (19 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 012 迁移、`user_detail_test.go` 与 `docs/impl/evidence/ADM-API-03c/`。
