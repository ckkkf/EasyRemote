# ADM-API-06a 管理员与角色（A14 后端）

> 来源：`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`（A14 管理员与角色）
> 需求：`docs/spec/pages.md` A14（行 268-272）、PRD §6.2（行 556-566）
> 前置：ADM-API-06 缺口卡、现有 `admin_users` 表与 `Authenticate` 强制 MFA

## 目标

落地 A14 的后端能力：管理员列表（角色 / 状态 / MFA 是否开启）、改角色、启用/禁用，全部写审计；并加两条防锁死约束：**不能移除最后一个 active 超管**（含禁用自己），有备份超管时允许自我降级。

**不做**：A14 页面（ADM-UI-09b 另开）、新建管理员/重置密码（需要邀请与密码重置流程，另卡）。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-21 `GET /admin/v1/admins` | 分页 | `list<AdminRow>`（id/username/role/status/mfa_enabled） | `admin.read`（仅 super） |
| ADM-22 `POST /admin/v1/admins/update` | `AdminUpdateRequest{admin_id, role?, status?, reason}` | `OkResult` | `admin.write`（仅 super） |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：50 条端点一致。
- [ ] `TestMemoryContract/AdminManagement`：列表按用户名排序、计数、改角色/状态、未知 id `ErrNotFound`。
- [ ] `TestAdminManagementAndGuards`：MFA 状态正确、ops 无 admin.read、改角色写审计、唯一超管禁用自己/被降级被拒、有备份超管时自我降级允许、缺原因 `ADMIN_REASON_REQUIRED`、非法角色 `ADMIN_CONFIG_INVALID`。
- [ ] `TestAdminEnvelopeContract`：新路由响应信封对齐。
- [ ] 既有回归（web 55 tests）与 `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`admins_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/tests/admin-scope.test.ts`（白名单 + 允许 /admins）
- `docs/impl/tasks/ADM-API-06a-admins.md`、`docs/impl/evidence/ADM-API-06a/**`

禁止：手改生成物；返回密码哈希或 TOTP 密钥；允许移除最后一个超管；改 `docs/spec/**` 的错误码表（本卡复用既有码）。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ ./internal/adminhttp/ -run 'AdminManagement|Envelope' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && bash scripts/gate.sh
```

## 完成标准

五条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 `admins_test.go` 与 `docs/impl/evidence/ADM-API-06a/`。
