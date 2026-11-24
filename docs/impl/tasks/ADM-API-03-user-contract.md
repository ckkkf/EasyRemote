# ADM-API-03 用户模块契约补齐（字段 / 筛选 / 按账号查询 / 导出）

> 来源：ADM-UI-05 执行时确认的缺口；`docs/spec/pages.md` A3（行 210-217）、A4（行 219-221）、PRD §6.3.2（行 585-592）
> 性质：**contract-first 后端卡**。契约先改、生成物再改、最后实现；前端只消费生成类型。

## 缺口（当前契约只有 UserRow{id, phone_mask, status, created_at}）

| 需求（pages.md） | 现状 | 需要的契约变化 |
|---|---|---|
| 列：昵称、设备数、最近登录 | 无字段 | `UserRow` 增 `nickname`、`device_count`、`last_login_at` |
| 筛选：昵称、设备码、注册时间范围 | 只有 `q` + `status` | `GET /admin/v1/users` 增 `nickname`、`device_code`、`created_from`、`created_to`（服务端过滤） |
| A4 最近 20 次会话（含质量） | 无按账号查询 | 新增 `GET /admin/v1/users/{id}/sessions`（分页 + 质量字段依赖 ADM-API-02） |
| A4 操作记录、备注 | 无 | 新增 `GET/PUT /admin/v1/users/{id}/notes`、操作记录可由审计按对象查询 |
| 解封、踢下线 | 只有封禁 | `POST /admin/v1/users/unban`、`POST /admin/v1/users/kick` |
| CSV 导出（进审计） | 无 | `GET /admin/v1/users/export`（服务端生成 + 审计 `user.export`） |

## 必须通过的断言（执行时）

- [ ] `node scripts/gen-contract.mjs --check` 与 `node scripts/check-routes.mjs --strict` 绿。
- [ ] `TestAdminEnvelopeContract` 覆盖新路由且信封对齐。
- [ ] `TestListUsers_Filters`：昵称/设备码/时间范围过滤由存储层实现（不是取一页再前端过滤）。
- [ ] `TestUsersExportWritesAudit`：导出动作写审计且 CSV 含脱敏手机号。
- [ ] `TestUnbanRequiresReason`：解封必须带原因并写审计。

## 只准改这些文件（执行时）

`docs/spec/contract.yaml`、`docs/spec/error-codes.md`（如需新码）、`server/internal/contract/**`（生成物）、`server/internal/adminapi/**`、`server/internal/adminhttp/**`、`server/internal/store/**`、`web-admin/src/contract.ts`（生成物）、对应测试。

## 状态

**未开始**。ADM-UI-05 已把入口做成"明确未开放/禁用"，不阻塞 UI 整改；本卡完成后再开 ADM-UI-05b 接真实字段。

## 进度

- 2026-11-23（ADM-API-03a）：`UserRow.device_count` 已落地（服务端一次聚合 `DeviceCountsByAccount`），用户列表与详情显示真实设备数。
- `nickname` / `last_login_at` **仍缺**：账号表没有这两列，需要 schema 迁移，单独排期。

## 进度（续）

- 2026-11-23（ADM-API-03b）：`nickname`、`last_login_at` 已落地（迁移 `003_account_profile.sql` + 登录路径写最近登录），列表 8 列、详情展示。剩余：按账号会话/操作记录/备注、解封/踢下线、CSV 导出。
