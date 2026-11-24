# ADM-UI-09d A11 版本管理页面

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-09d）
> 需求：`docs/spec/pages.md` A11（行 254）、PRD §6.3.6（行 621-623）
> 前置：ADM-API-06b（`GET/POST /admin/v1/versions`、`/versions/retire`）

## 目标

A11 从"未开放"变为可用页面：版本列表（平台/版本号/状态/覆盖率/强制升级/更新日志/更新时间）、发布（草稿/灰度/全量，覆盖率与状态一致）、下线（原因必填）；页面受 `version.read` 守卫。

## 输入 / 输出

| 交互 | 接口 |
|---|---|
| 列表 | `GET /admin/v1/versions?limit=50` |
| 发布/更新 | `POST /admin/v1/versions {platform, version, status, rollout_percent, force_update, notes}` |
| 下线 | `POST /admin/v1/versions/retire {version_id, reason}` |

前端预校验（与后端 `validateRollout` 同规则）：draft=0、rolling=1–99、full=100；服务端仍是唯一权威。

## 必须通过的断言

- [ ] `npm test -- --run`：路由 9 项、`/versions` 受 `version.read` 保护。
- [ ] `TestVersionsE2E`：导航出现版本管理；初始空；发布草稿（0%）；全量 50% 被前端拦截并给出说明；灰度 20% 发布成功；发布写 `version.publish` 审计；下线需原因并写 `version.retire`；深链刷新；无权限无入口且深链 403。
- [ ] 六屏判据：仅 `a2-dashboard.nav` 增加一项（预期），新 manifest 为基线。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `web-admin/src/app/routes.ts`、`src/api.ts`、`src/App.tsx`
- `web-admin/tests/{routes,admin-scope}.test.ts`
- `scripts/admin-ui-e2e-versions.py`（新增）
- `docs/impl/tasks/ADM-UI-09d-versions.md`、`docs/impl/evidence/ADM-UI-09d/**`
- `docs/impl/evidence/ADM-UI-00/page-matrix.md`（A11 → implemented）

禁止：改契约；前端本地伪造版本行；允许 full 状态覆盖率不足 100。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-versions.py
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (14 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` routes/api/App/tests；删除 `scripts/admin-ui-e2e-versions.py` 与 `docs/impl/evidence/ADM-UI-09d/`；矩阵 A11 回退为 blocked-by-api。
