# ADM-UI-09a 能力核对：A10 / A11 / A14 保持未开放

> 卡：`docs/impl/tasks/ADM-UI-09a.md`
> 核对时间：2026-11-23
> 结论：**这三页当前没有任何后端接口，管理端不提供入口（明确未开放），并加自动断言防止假入口。**

## 1. 契约证据

```text
$ grep -n 'path: /admin' docs/spec/contract.yaml
214:    path: /admin/v1/login
220:    path: /admin/v1/logout
226:    path: /admin/v1/stats/overview
232:    path: /admin/v1/users
238:    path: /admin/v1/users/ban
244:    path: /admin/v1/devices
250:    path: /admin/v1/sessions
256:    path: /admin/v1/sessions/kill
262:    path: /admin/v1/config
268:    path: /admin/v1/config
274:    path: /admin/v1/audit
```

没有 `/admin/v1/risk/*`、`/admin/v1/versions/*`、`/admin/v1/admins/*`。

## 2. 服务端证据

```text
$ grep -rn 'mux.HandleFunc' server/internal/adminhttp/handler.go
"/healthz" / "/admin/v1/login" / "/admin/v1/logout" / "/admin/v1/stats/overview"
"/admin/v1/users" / "/admin/v1/users/ban" / "/admin/v1/devices"
"/admin/v1/sessions" / "/admin/v1/sessions/kill" / "/admin/v1/config" / "/admin/v1/audit"
```

与契约一一对应，没有风控/版本/管理员处理器。

## 3. 页面需求对照（pages.md）

| 页面 | 需求 | 现状 | 处置 |
|---|---|---|---|
| A10 风控中心（V1） | 异常连接告警、黑名单（CIDR + 有效期）、爆破记录、举报工单、风险评分 | 无任何接口 | **未开放**；不做假入口 |
| A11 版本管理 | 版本列表、灰度发布、强制升级阈值、更新日志 | 无任何接口 | **未开放**；前端不能"发布" |
| A14 管理员与角色 | 管理员列表、角色分配、MFA 状态、禁用 | 无接口（角色只在登录响应里） | **未开放**；RBAC 仍由服务端 `adminapi.Can` 唯一决定 |

## 4. 管理端当前行为

- 导航只渲染 `visibleRoutes(identity.permissions)`，即 6 个真实页面：仪表盘/用户/设备/会话/审计日志/配置中心。
- `web-admin/src/App.tsx` 中不存在「风控中心」「版本管理」「管理员与角色」字样，也没有禁用的占位导航。
- 自动断言：`web-admin/tests/admin-scope.test.ts`
  - `TestAdminRoutesMatchImplementedPages`
  - `TestNoFakeAdminNavEntries`
  - `TestContractHasNoRiskVersionAdminEndpoints`
  - `TestUnopenedPagesDocumented`

## 5. 后续

接口补齐见 `docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`。接口落地后，再按 `pages.md` A10/A11/A14 各开独立页面卡，并同步更新 `admin-scope.test.ts` 的白名单（否则测试会拦住）。
