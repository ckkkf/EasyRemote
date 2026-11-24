# ADM-API-06 风控中心 / 版本管理 / 管理员与角色契约

> 来源：ADM-UI-09a 能力核对；`docs/spec/pages.md` A10（行 248-250）、A11（行 252-254）、A14（行 268-272）、PRD §6.3.5（行 609-617）、§6.3.6（行 619-624）、§6.2（行 556-566）
> 性质：**contract-first 后端卡**。当前这三页**没有任何后端接口**，所以管理端保持未开放。

## 现状证据（2026-11-23）

- `docs/spec/contract.yaml` 的 admin 路由只有 10 条：login / logout / stats/overview / users / users/ban / devices / sessions / sessions/kill / config(GET/PUT) / audit。
- `server/internal/adminhttp/handler.go` 的 `mux.HandleFunc` 与契约一一对应，没有 risk/version/admin 相关处理器。
- 因此 A10/A11/A14 没有数据源，做页面只能是假入口。

## 需要补的契约

| 页面 | 能力 | 端点 |
|---|---|---|
| A10 风控 | 异常连接告警列表 | `GET /admin/v1/risk/alerts` |
| A10 风控 | 黑名单（IP/设备码/账号，CIDR + 有效期） | `GET/POST/DELETE /admin/v1/risk/blocklist` |
| A10 风控 | 验证码爆破记录 | `GET /admin/v1/risk/captcha-failures` |
| A11 版本 | 版本列表/状态/覆盖率 | `GET /admin/v1/versions` |
| A11 版本 | 灰度发布（5/20/50/100% 或白名单） | `POST /admin/v1/versions/{version}/rollout` |
| A11 版本 | 强制升级阈值 | `PUT /admin/v1/versions/policy` |
| A14 管理员 | 管理员列表 / 角色 / MFA 状态 | `GET /admin/v1/admins` |
| A14 管理员 | 角色分配、禁用（audited） | `PUT /admin/v1/admins/{id}` |

## 必须通过的断言（执行时）

- [ ] 每个写操作必须带原因/二次确认并写审计（`risk.blocklist.add`、`version.rollout`、`admin.role_change`）。
- [ ] A14 必须复用 `adminapi.Can` 的权限模型，不允许前端硬编码角色能力。
- [ ] A11 的灰度发布必须能被客户端真实读取（`/v1/config` 或版本检查接口），否则只是改数据库。
- [ ] `TestAdminScopeGrowsWithContract`：新增页面时同步更新 `web-admin/tests/admin-scope.test.ts` 的路由与契约白名单。

## 状态

**未开始**。管理端不提供入口；`admin-scope.test.ts` 会在有人偷偷加假入口时失败。

## 进度

- 2026-11-23（ADM-API-06a）：A14 后端已落地——`GET /admin/v1/admins`（角色/状态/MFA）、`POST /admin/v1/admins/update`（改角色/启停，原因必填 + 审计），含「最后一个超管」防锁死。
- 仍缺：A14 页面（ADM-UI-09b）、新建管理员/重置密码流程、A10 风控、A11 版本管理。

## 进度（续）

- 2026-11-23（ADM-API-06b）：A11 后端已落地——版本列表/发布（灰度覆盖率校验）/下线，写操作进审计。
- 仍缺：A11 页面、客户端检查更新接口、白名单灰度、A10 风控。

## 进度（续）

- 2026-11-23（ADM-API-06c）：A10 可执行部分落地——黑名单（账号/设备/IP+CIDR+有效期）、连接闸门（命中拒绝并写告警）、告警列表。
- 仍缺：A10 页面、验证码爆破记录、举报工单、自动规则引擎。
