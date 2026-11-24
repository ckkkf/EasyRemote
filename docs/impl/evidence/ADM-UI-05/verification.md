# ADM-UI-05a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-05a.md`
> 时间：2026-11-23 11:16-11:26（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/components/business/data-table.tsx` | 表格唯一实现（列定义 + 行渲染 + 空态 + 可点击行 + aria-label） |
| `web-admin/src/components/business/pill.tsx` | 状态胶囊从 App.tsx 抽出，详情页与列表共用 |
| `web-admin/src/features/users/user-detail-page.tsx` | A4 独立路由详情：账号信息（真实字段）+ 绑定设备（服务端按账号过滤）+ 未开放能力的明确说明 |
| `web-admin/src/app/routes.ts` | 子路径归属父页面：`/users/:id` 的标题/权限/导航高亮按「用户」 |
| `web-admin/src/App.tsx` | Users 列表改 DataTable；行点击改路由跳转；删除 UserDetail modal；新增 `:id` 路由；导出/更多按钮禁用并注明原因 |
| `web-admin/tests/data-table.test.tsx`、`tests/routes.test.ts` | 3 条表格断言 + 子路径解析断言 |
| `scripts/admin-ui-e2e-users.py` | 12 条真实 E2E |
| `docs/impl/tasks/ADM-API-03-user-contract.md` | 缺失字段/筛选/按账号查询/导出/解封踢下线的 contract-first 卡 |

## 2. 字段映射

| 展示项 | 来源 |
|---|---|
| 账号 ID / 手机号（脱敏）/ 状态 / 注册时间 | `UserRow` |
| 绑定设备 | `GET /admin/v1/devices?q=<account_id>`（服务端过滤 + 本地精确校验） |
| 昵称 / 最近登录 / 设备数 / 历史会话数 | 契约无字段 → 页面写明「接口未提供（ADM-API-03）」 |
| 按账号会话 / 操作记录 / 备注 | 无接口 → 「接口未开放」 |
| CSV 导出 / 解封 / 踢下线 / 备注 | 无接口 → 按钮禁用 + title 说明 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 列/行渲染、空态、可点击行 | `TestDataTableRendersColumnsAndRows` / `TestDataTableEmptyState` / `TestDataTableClickableRows` | PASS |
| `/users/:id` 归属用户页、`/config/x` 无权限 forbidden | `TestRouteResolution`（扩展） | PASS |
| 列表/详情/深链/未开放/未知账号 | `TestUsersE2E` | PASS 12/12 |
| 六屏 DOM 判据不变 | `TestVisualBaselineUnchanged` | PASS |
| 回归 / 技术栈 / 闸门 | `npm test`（32）/ `stack-check`（4/4）/ `gate.sh` | 全绿 |

## 4. 关键输出

```text
Tests  32 passed (32)
PASS users table columns -> ['账号 ID', '手机号', '状态', '注册时间', '操作']
PASS phone masked -> ['139****5000', '139****5001']
PASS row opens detail route -> http://127.0.0.1:5398/users/acc-dce11279a822fbd6
PASS detail lists devices -> 2
PASS deep link survives reload
E2E OK (12 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

截图：`users-list.png`（禁用导出/更多）、`user-detail.png`（`139****5000`、2 台设备、未开放说明）。

## 5. 未完成项、限制与风险

- **列/筛选/写操作/导出仍缺接口**：昵称、设备数、最近登录、昵称/设备码/时间范围筛选、按账号会话、操作记录、备注、解封、踢下线、CSV 导出全部依赖 ADM-API-03；本卡只做禁用与说明，**没有前端拼数据**。
- 详情页的"绑定设备"是真实服务端过滤（`q=<account_id>`）+ 本地精确校验；会话不做前端过滤，避免用第一页数据冒充"该账号的会话"。
- 列表"更多"按钮当前禁用；等 ADM-API-03 后再逐项开放并补审计断言。
- 视觉：列表列/行/分页判据与 ADM-UI-04 基线一致；详情页是新页面，截图见证据目录。

## 6. 回滚

删除 `data-table.tsx`、`pill.tsx`、`user-detail-page.tsx`、`tests/data-table.test.tsx`、`scripts/admin-ui-e2e-users.py`、`docs/impl/evidence/ADM-UI-05/`；`git checkout -- web-admin/src/App.tsx web-admin/src/app/routes.ts web-admin/tests/routes.test.ts`。
