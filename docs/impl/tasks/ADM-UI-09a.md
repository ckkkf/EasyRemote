# ADM-UI-09a 风控 / 版本 / 管理员：能力核对与未开放决策

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-09（行 178-184）
> 需求：`docs/spec/pages.md` A10（行 248-250）、A11（行 252-254）、A14（行 268-272）、PRD §6.2/§6.3.5/§6.3.6
> 前置：ADM-UI-00 矩阵已把这三页标为 `blocked-by-api`

## 目标

核对 A10/A11/A14 的 API 能力，确认三者**没有任何后端接口**，因此管理端保持"明确未开放"：不加入导航、不做禁用占位入口、不写前端假数据；同时加自动断言，防止后续有人偷偷加假入口或偷偷改契约白名单。

## 输入 / 输出

不改 API、不改契约、不改任何页面实现（本卡只加测试与文档）。字段映射 **N/A**。

## 必须通过的断言

- [ ] `TestAdminRoutesMatchImplementedPages`：路由表恰为 6 个有接口的页面，路径不含 risk/version/admin/role/blacklist。
- [ ] `TestNoFakeAdminNavEntries`：`App.tsx` 不含「风控中心」「版本管理」「管理员与角色」，导航只来自 `visibleRoutes`。
- [ ] `TestContractHasNoRiskVersionAdminEndpoints`：admin 契约路径恰为已知 11 条（config 两条），无 risk/versions/admins/roles/blacklist。
- [ ] `TestUnopenedPagesDocumented`：`ADM-API-06` 卡与 `capability-audit.md` 存在且覆盖 A10/A11/A14。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-09a.md`、`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`
- `docs/impl/evidence/ADM-UI-09/**`
- `web-admin/tests/admin-scope.test.ts`

禁止：新增路由/导航/页面骨架；改 `docs/spec/**`、`server/**`、`prototype/**`。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && bash scripts/gate.sh
```

## 完成标准

四条断言全绿；gate `GATE: PASS`；证据落 `docs/impl/evidence/ADM-UI-09/`。

## 回滚

删除 `web-admin/tests/admin-scope.test.ts`、`docs/impl/evidence/ADM-UI-09/`、`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`。
