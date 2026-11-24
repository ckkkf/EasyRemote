# ADM-UI-02a 路由与权限守卫（App Shell 第一步）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-02（行 121-127）
> 需求：PRD §6.4（行 666 React Router）、§6.7.1（行 704-708）
> 前置：ADM-UI-00（基线）、ADM-UI-01a（React 18 + Tailwind + primitives）已完成
> 拆分说明：ADM-UI-02 同时包含路由、Query、AdminShell 视觉迁移、DataTable 等共享组件。本卡**只做路由与守卫**，页面组件、视觉、请求时机零改动；Shell 视觉迁移与共享组件按 ADM-UI-02b/02c 另卡执行。

## 目标

把 `useState<Page>` 的本地页面切换换成 React Router：URL 成为页面状态的唯一来源，刷新与深链不丢页，未登录访问业务页回到登录页，无权限页面渲染明确的「无权限」而不是白屏，未知路径渲染 404。

## 输入 / 输出

不改 API、不改契约、不改生成物，字段映射 **N/A**。

## 必须通过的断言

- [ ] `TestAdminRouteTableCoversNav`（`tests/routes.test.ts`）：6 个页面 id 与路由表一一对应；路径唯一、以 `/` 开头；`childPath()` 为相对路径；`routeById`/`routeByPath` 双向可查。
- [ ] `TestRoutePathsMatchDeepLinks`（同上）：路径固定为 `/dashboard /users /devices /sessions /audit /config`，`HOME_PATH=/dashboard`、`LOGIN_PATH=/login`（深链是对外契约，改路径必须改测试）。
- [ ] `TestRouteResolution`（同上）：`/login`→login、`/`→home、`/users/` 与 `/users?x=1`→page、`/nope`→notfound、`/config` 无权限→forbidden、有 `config.read`→page。
- [ ] `TestPermissionGateOnlyConfig`（同上）：无权限时只有 config 被拦；`visibleRoutes([])` 不含 config。**不新增服务端不存在的权限动作**。
- [ ] `TestNormalizePath`（同上）：空串、`/`、尾斜杠、hash 归一。
- [ ] `TestAdminDeepLinkE2E`（`python3 scripts/admin-ui-e2e-routes.py`）：真实 stack + 真实登录 + 真实构建产物，14 条检查全 PASS —— 深链 `/users` 渲染用户页且有数据行、刷新保持路由、`/config` 渲染配置表、`/nope` 渲染「页面不存在」、导航点击改 URL 且高亮跟随、已登录访问 `/login` 回仪表盘、未登录深链落登录页且不渲染业务表格、`pageErrors` 为空。
- [ ] 既有回归全绿：`npx tsc --noEmit`、`npm test -- --run`、`npm run build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-02a.md`、`docs/impl/evidence/ADM-UI-02a/**`
- `web-admin/package.json`、`web-admin/package-lock.json`（react-router-dom、@tanstack/react-query）
- `web-admin/src/app/routes.ts`（路由表 + 解析/守卫纯函数）
- `web-admin/src/app/providers.tsx`（QueryClientProvider + 权限上下文）
- `web-admin/src/main.tsx`（BrowserRouter）
- `web-admin/src/App.tsx`（页面切换改路由；页面组件内部不改）
- `web-admin/tests/routes.test.ts`
- `scripts/admin-ui-e2e-routes.py`

禁止：改 `web-admin/src/styles.css`、改任何页面组件的列/筛选/请求逻辑、改 `docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止用 `HashRouter` 绕过服务端 SPA 回退；禁止 `git commit/revert/stash`。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-routes.py
bash scripts/gate.sh
```

## 完成标准

七条断言全绿；E2E 输出 `E2E OK (14 checks)`；gate `GATE: PASS`；证据落 `docs/impl/evidence/ADM-UI-02a/`。

## 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/main.tsx web-admin/package.json web-admin/package-lock.json`，删除 `web-admin/src/app/`、`web-admin/tests/routes.test.ts`、`scripts/admin-ui-e2e-routes.py`、`docs/impl/evidence/ADM-UI-02a/`，然后 `cd web-admin && npm ci`。
