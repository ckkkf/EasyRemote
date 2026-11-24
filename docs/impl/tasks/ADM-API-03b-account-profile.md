# ADM-API-03b 账号资料列（nickname / last_login_at）

> 来源：`docs/impl/tasks/ADM-API-03-user-contract.md`（列：昵称、最近登录）
> 需求：`docs/spec/pages.md` A3（行 213）、PRD §6.3.2（行 588）
> 前置：ADM-API-03a（device_count）、ADM-02b（登录路径回填）

## 目标

给账号表加 `nickname`、`last_login_at` 两列（幂等迁移），契约暴露 `UserRow.nickname` 与 `UserRow.last_login_at`；最近登录由**登录成功路径**写入，管理端列表与详情展示。

## 输入 / 输出

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `data.items[].nickname` | string | 运营设置的昵称；空串表示未设置（没有编辑接口，不造假名） | 契约 `UserRow` |
| `data.items[].last_login_at` | time? | 最近一次成功登录；历史账号为 0001-01-01（UI 显示「—」） | 契约 `UserRow`；登录路径写入 |

## 必须通过的断言

- [ ] `node scripts/gen-contract.mjs --check`：三端一致。
- [ ] `TestMemoryContract/AccountLastLogin`：写入成功；未知账号 `ErrNotFound`。
- [ ] `TestLoginRecordsLastLoginAt`：登录后返回值与存储的 `LastLoginAt` 都非零。
- [ ] `TestUsersE2E`：8 列（含昵称、最近登录）；昵称为「—」（未设置）；最近登录为「刚刚」；手机号仍脱敏；设备数 2/1。
- [ ] `TestVisualBaselineUnchanged`：与 ADM-API-03a 基线相比**只有 a3 列变化**（有意）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `migrations/003_account_profile.sql`（新增）
- `server/internal/stack/stack_pg.go`（多迁移文件按序拼接）
- `server/internal/store/{store,memory,pg}.go`、`storetest/storetest.go`
- `server/internal/auth/auth.go`、`auth_test.go`
- `server/internal/adminapi/adminapi.go`、`adminapi_test.go`
- `web-admin/src/api.ts`、`App.tsx`、`features/users/user-detail-page.tsx`
- `scripts/admin-ui-e2e-users.py`
- `docs/impl/tasks/ADM-API-03b-account-profile.md`、`docs/impl/evidence/ADM-API-03b/**`

禁止：手改生成物；用前端时间冒充最近登录；为昵称编默认值。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/store/ ./internal/auth/ ./internal/adminapi/ -run 'Contract|LoginRecords|ListUsers' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-users.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-API-03b/screens --compare-to docs/impl/evidence/ADM-API-03a/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；视觉差异只有 a3 列；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约、生成物、store/auth/adminapi 六个文件、前端三个文件、E2E 脚本；删除 `migrations/003_account_profile.sql` 与 `docs/impl/evidence/ADM-API-03b/`。
