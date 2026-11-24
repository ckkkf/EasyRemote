# ADM-UI-05a 用户列表 DataTable + 详情独立路由

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-05（行 145-152）
> 需求：`docs/spec/pages.md` A3（行 210-217）、A4（行 219-221）、PRD §6.3.2（行 585-592）
> 前置：ADM-UI-02a/02b/02c（路由/三态/对话框）、ADM-02a（phone_mask 已修复）
> 拆分说明：ADM-UI-05 的列/筛选/写操作/导出依赖 ADM-API-03 契约。本卡只做**不依赖新接口**的部分：统一 DataTable、详情从 modal 改为独立路由（可深链/刷新）、未开放能力显式禁用并写明原因。

## 目标

1. 用户列表改用 `components/business/data-table.tsx`（PRD 要求表格唯一实现），列与分页行为不变；
2. 用户详情从 modal 改为 `/users/:id` 独立路由：账号信息 + 绑定设备（服务端按账号过滤），会话/操作记录/备注明确「接口未开放」；
3. 解封/踢下线/备注/CSV 导出等无接口能力：**禁用按钮 + 明确原因**，不做可点击假入口。

## 输入 / 输出

不改 API、不改契约。字段映射：

| 展示项 | 来源 |
|---|---|
| 账号 ID / 手机号（脱敏）/ 状态 / 注册时间 | `UserRow`（`contract.yaml`） |
| 绑定设备 | `GET /admin/v1/devices?q=<account_id>`（服务端过滤 + 本地精确校验） |
| 昵称 / 最近登录 / 设备数 / 历史会话数 | 契约无字段 → 页面写明「接口未提供（ADM-API-03）」 |
| 按账号的会话 / 操作记录 / 备注 | 无接口 → 「接口未开放」 |
| CSV 导出 / 解封 / 踢下线 / 备注 | 无接口 → 按钮禁用 + title 说明 |

## 必须通过的断言

- [ ] `TestDataTableRendersColumnsAndRows`：列头/行渲染/表头行数正确，无 onRowClick 时不带 clickable。
- [ ] `TestDataTableEmptyState`：空数据渲染调用方给的空态。
- [ ] `TestDataTableClickableRows`：有 onRowClick 时行可点击并带 title。
- [ ] `TestRouteResolution`（扩展）：`/users/acc-1` 归属 users 页面；`/config/x` 无权限时 forbidden。
- [ ] `TestUsersE2E`（`python3 scripts/admin-ui-e2e-users.py`）：12 条全 PASS —— 列、脱敏、导出/更多禁用、行点击进 `/users/<id>`、账号信息、脱敏手机号、绑定设备 2 台、会话/备注「接口未开放」、深链刷新仍在详情、返回列表、未知账号「用户不存在」、无 pageerror。
- [ ] `TestVisualBaselineUnchanged`：六屏 DOM 判据与 ADM-UI-04 基线一致（列表列/行/分页未变）。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-05a.md`、`docs/impl/tasks/ADM-API-03-user-contract.md`、`docs/impl/evidence/ADM-UI-05/**`
- `web-admin/src/components/business/data-table.tsx`、`pill.tsx`（新增）
- `web-admin/src/features/users/user-detail-page.tsx`（新增）
- `web-admin/src/app/routes.ts`（子路径归属父页面）
- `web-admin/src/App.tsx`（Users 列表改 DataTable、删除 UserDetail modal、新增 `:id` 路由）
- `web-admin/tests/data-table.test.tsx`、`tests/routes.test.ts`
- `scripts/admin-ui-e2e-users.py`

禁止：改其它页面、改 `docs/spec/**`、`server/**`、`prototype/**`；禁止用前端过滤冒充服务端按账号查询；禁止把未开放能力做成可点击按钮。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-users.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-05/screens --compare-to docs/impl/evidence/ADM-UI-04/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (12 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

删除 `data-table.tsx`、`pill.tsx`、`user-detail-page.tsx`、`tests/data-table.test.tsx`、`scripts/admin-ui-e2e-users.py`、`docs/impl/evidence/ADM-UI-05/`；`git checkout -- web-admin/src/App.tsx web-admin/src/app/routes.ts web-admin/tests/routes.test.ts`。
