# ADM-API-06a 验证记录（管理员与角色）

> 卡：`docs/impl/tasks/ADM-API-06a-admins.md`
> 时间：2026-11-23 13:30-14:02（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `AdminRow`/`AdminUpdateRequest` + ADM-21/22 |
| `store/{store,memory,pg}.go`、`storetest` | `GetAdmin`/`ListAdmins`/`CountAdmins`/`UpdateAdminRole`/`UpdateAdminStatus` |
| `adminapi/adminapi.go`、`admins_test.go` | `ListAdmins`/`CountAdmins`/`UpdateAdmin`（权限、原因、审计、防锁死） |
| `adminhttp/handler.go` | 两条路由（分页信封 / OkResult） |
| `web-admin/tests/admin-scope.test.ts` | 白名单加 /admins，并把 /admins 从"禁止出现"名单移除（页面仍未开放） |
| `docs/impl/tasks/ADM-API-06a-admins.md`、`docs/impl/evidence/ADM-API-06a/**` | 卡片与证据 |

## 2. 字段映射

| 字段/端点 | 说明 |
|---|---|
| `AdminRow{id, username, role, status, mfa_enabled}` | `mfa_enabled = TOTPSecret != ""`；**不返回**密码哈希与 TOTP 密钥 |
| ADM-21 `GET /admin/v1/admins` | 分页（limit/offset），按 username 升序；权限 `admin.read`（super） |
| ADM-22 `POST /admin/v1/admins/update` | `{admin_id, role?, status?, reason}` → `OkResult`；权限 `admin.write`（super） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（50 端点） |
| 列表/计数/改角色/启停/未知 id | `TestMemoryContract/AdminManagement` | PASS |
| MFA 状态、权限、审计、防锁死 | `TestAdminManagementAndGuards` | PASS |
| 信封契约 | `TestAdminEnvelopeContract` | PASS（含两条新路由） |
| web 回归 | `npm test -- --run` | PASS 55/55 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（50 个类型 / 50 条接口）
✓ 路由 method/path/auth 与契约一致（50 条端点）
--- PASS: TestMemoryContract/AdminManagement
--- PASS: TestAdminManagementAndGuards
--- PASS: TestAdminEnvelopeContract
GATE: PASS
```

## 5. 设计取舍（防锁死）

最初实现"一律禁止修改自己"，导致"最后一个超管"保护不可达、且过于粗暴。改为**语义约束**：
任何会移除 active 超管的操作（降级或禁用），都必须保证还剩另一个 active 超管；因此
- 唯一超管禁用/降级自己 → 拒绝（`ADMIN_FORBIDDEN`，审计 result=`last_super_protected`）；
- 已有备份超管时，自我降级允许（交接场景）；
- 禁用后登录会被 `Authenticate` 的 status 检查拦住（已存在的真实闭环，非本卡新增）。

## 6. 未完成项、限制与风险

- **A14 页面未做**（ADM-UI-09b）；`admin-scope.test.ts` 仍守护"导航里没有管理员与角色"。
- 新建管理员、重置密码、MFA 重置流程未做（需要邀请与恢复流程，另卡）。
- 管理员列表无 created_at（`admin_users` 表无该列），页面不能显示"创建时间"，需要迁移卡。
- 审计 action 复用 `admin.update`，before/after 记 `role/status`；细分角色/状态两类动作留待需要时再拆。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 `admins_test.go` 与 `docs/impl/evidence/ADM-API-06a/`。
