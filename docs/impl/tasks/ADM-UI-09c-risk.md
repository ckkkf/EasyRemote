# ADM-UI-09c A10 风控中心页面

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-09c）
> 需求：`docs/spec/pages.md` A10（行 250）、PRD §6.3.5（行 609-617）
> 前置：ADM-API-06c（黑名单 CRUD + 告警 + 连接闸门）

## 目标

把 A10 从"未开放"变成可用页面：黑名单列表/新增/移除（账号、设备码、IP+CIDR、有效期）与异常连接告警列表；写操作填原因并落审计；页面受 `risk.read` 守卫。

## 输入 / 输出

| 交互 | 接口 |
|---|---|
| 黑名单列表 | `GET /admin/v1/risk/blacklist?limit=50` |
| 新增 | `POST /admin/v1/risk/blacklist {kind, value, note, ttl_hours}` |
| 移除 | `POST /admin/v1/risk/blacklist/remove {id, reason}` |
| 告警 | `GET /admin/v1/risk/alerts?limit=50` |

## 必须通过的断言

- [ ] `npm test -- --run`：路由 8 项、`/risk` 受 `risk.read` 保护。
- [ ] `TestRiskE2E`：导航出现风控中心；初始黑名单为空；非法 IP 被服务端拒绝（`ADMIN_CONFIG_INVALID`）；新增账号黑名单（24h 到期）写 `risk.blacklist_add` 审计；真实拦截产生的告警在页面可见（`account:` 主体）；移除需原因并写 `risk.blacklist_remove`；无 `risk.read` 无入口且深链 403。
- [ ] 六屏判据：仅 `a2-dashboard.nav` 增加一项（预期），新 manifest 为基线。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `web-admin/src/app/routes.ts`、`src/api.ts`、`src/App.tsx`
- `web-admin/tests/{routes,admin-scope}.test.ts`
- `scripts/admin-ui-e2e-risk.py`（新增）
- `docs/impl/tasks/ADM-UI-09c-risk.md`、`docs/impl/evidence/ADM-UI-09c/**`
- `docs/impl/evidence/ADM-UI-00/page-matrix.md`（A10 → implemented）

禁止：改契约；前端本地伪造告警；给无权限角色显示入口。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-risk.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (14 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` routes/api/App/tests；删除 `scripts/admin-ui-e2e-risk.py` 与 `docs/impl/evidence/ADM-UI-09c/`；矩阵 A10 回退为 blocked-by-api。
