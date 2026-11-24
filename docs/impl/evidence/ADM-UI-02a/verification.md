# ADM-UI-02a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-02a.md`
> 时间：2026-11-23 10:30-10:38（Asia/Shanghai）
> 基线 commit：`267a635`

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/app/routes.ts` | 路由表 + `normalizePath` / `routeById` / `routeByPath` / `childPath` / `canAccess` / `visibleRoutes` / `resolvePath` 纯函数；导航、标题、守卫、深链同源 |
| `web-admin/src/app/providers.tsx` | `QueryClientProvider`（重取/重试/staleTime 显式配置）+ 权限上下文 `useCan` |
| `web-admin/src/main.tsx` | `BrowserRouter` 包裹（不用 HashRouter，深链走服务端 SPA 回退） |
| `web-admin/src/App.tsx` | `useState<Page>` → 布局路由 + `<Outlet/>`；导航改 `NavLink`；登录/登出走 `navigate`；新增 Forbidden / NotFound；页面组件内部零改动 |
| `web-admin/package.json`、`package-lock.json` | `react-router-dom ^6.30.6`、`@tanstack/react-query ^5.108.3` |
| `web-admin/tests/routes.test.ts` | 5 条路由/权限断言 |
| `scripts/admin-ui-e2e-routes.py` | 真实 stack + 真实登录的 14 条深链/守卫 E2E |

未改：`src/styles.css`、任何页面的列/筛选/请求逻辑、`docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`。

## 2. 字段映射

N/A（不改任何 API/契约/生成物）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 路由表覆盖 6 个导航项、双向可查、childPath 相对 | `TestAdminRouteTableCoversNav` | PASS |
| 深链路径冻结为 6 条 | `TestRoutePathsMatchDeepLinks` | PASS |
| 解析：login/home/page/notfound/forbidden + 尾斜杠/query | `TestRouteResolution` | PASS |
| 只有 config 需要权限，不新增服务端没有的动作 | `TestPermissionGateOnlyConfig` | PASS |
| 路径归一 | `TestNormalizePath` | PASS |
| 深链/刷新/导航/守卫/404（真实 API） | `TestAdminDeepLinkE2E` | PASS（14/14，`E2E OK (14 checks)`） |
| 既有回归 | `npm test -- --run` | 6 files / 15 tests PASS |
| 类型/构建/闸门 | `tsc --noEmit` / `npm run build` / `bash scripts/gate.sh` | 全绿，末行 `GATE: PASS` |

E2E 逐条（`e2e-routes.json`）：

1. 深链 `/users` 渲染「用户」页，URL 保持 `/users`，表格有数据行；
2. 刷新后仍在 `/users`（改造前会回仪表盘）；
3. 深链 `/config` 渲染配置表（root 有 `config.read`）；
4. `/nope` 渲染「页面不存在」，不是白屏；
5. 点击导航「审计日志」→ URL `/audit`、标题更新、`nav.active` 跟随；
6. 已登录访问 `/login` → 重定向 `/dashboard`；
7. 未登录深链 `/users` → 落 `/login`，页面无业务表格；
8. `pageErrors` 为空。

## 4. 命令与关键输出

见 `commands.txt`（tsc / vitest / build / E2E / gate 原始输出）。关键行：

```text
Test Files  6 passed (6)
     Tests  15 passed (15)
PASS deep link /users renders page -> 用户
PASS reload keeps route -> http://127.0.0.1:5394/users
PASS unknown path renders notfound -> 页面不存在
E2E OK (14 checks)
GATE: PASS
```

## 5. 未完成项、限制与风险

- 本卡只做路由与守卫：AdminShell 视觉、DataTable、Loading/Empty/Error/ConfirmDialog 共享组件、页面迁 `useQuery` 仍属 ADM-UI-02b/02c。
- 页面请求仍是各自 `useEffect`；`QueryClientProvider` 已接线但页面尚未使用 `useQuery`（避免本卡改动请求时机）。
- 权限入口只对齐现状（config 需要 `config.read`）。**服务端仍是唯一授权点**：前端隐藏不是授权，越权请求照样被 adminapi 拒绝。
- 登录页的开发口令提示（`App.tsx` 内 `开发环境：root / dev-pass`）仍在，属 ADM-UI-03。
- **闸门抖动记录**：10:36 一次 `gate.sh` 在 `flutter-test` 失败（当时并行了 E2E 与 npm 构建）；单独跑 `--only=flutter-test` 与串行全量 `gate.sh` 均 PASS。与 web-admin 改动无关，属客户端 E2E 在并发负载下的不稳定，需单独建卡排查（未在本卡处理）。

## 6. 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/main.tsx web-admin/package.json web-admin/package-lock.json`，删除 `web-admin/src/app/`、`web-admin/tests/routes.test.ts`、`scripts/admin-ui-e2e-routes.py`、`docs/impl/evidence/ADM-UI-02a/`，再 `cd web-admin && npm ci`。
