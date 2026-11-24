# ADM-UI-09d 验证记录（A11 版本管理页面）

> 卡：`docs/impl/tasks/ADM-UI-09d-versions.md`
> 时间：2026-11-23 14:32-14:40（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/app/routes.ts` | 新增 `versions` 路由（`/versions`，权限 `version.read`） |
| `web-admin/src/api.ts` | `versions/publishVersion/retireVersion` + `ClientVersionRow` |
| `web-admin/src/App.tsx` | `Versions` 页面：列表 + 发布对话框（含覆盖率预校验）+ 下线对话框 |
| `web-admin/tests/{routes,admin-scope}.test.ts` | 路由 9 项、`/versions` 权限守卫、A11 移出"未开放" |
| `scripts/admin-ui-e2e-versions.py` | 14 条 E2E |
| `page-matrix.md` | A11 → implemented |

## 2. 字段映射

| 交互 | 接口字段 |
|---|---|
| 列表 | `ClientVersionRow{id, platform, version, status, rollout_percent, force_update, notes, created_at, updated_at}` |
| 发布 | `{platform, version, status, rollout_percent, force_update, notes}` → `ClientVersionRow` |
| 下线 | `{version_id, reason}` → `OkResult` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 路由表/权限守卫 | `TestAdminRouteTableCoversNav`、`TestPermissionGateOnlyConfig` | PASS |
| A11 已开放且受 `version.read` | `TestAdminRoutesMatchImplementedPages`、`TestNoFakeAdminNavEntries` | PASS |
| 端到端 | `TestVersionsE2E` | PASS 14/14 |
| 六屏判据 | `TestVisualBaselineUnchanged` | 仅 nav 增加一项（预期），新 manifest 为基线 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
PASS draft published -> windows 0.2.14 草稿 0% 否 — ...
PASS full with 50% blocked locally
PASS rollout error explained
PASS rolling published with 20%
PASS publish writes audit
PASS retire requires reason
PASS retire marks version retired
PASS retire writes audit
PASS no versions nav without permission
PASS forbidden on deep link
E2E OK (14 checks)
VISUAL DIFF: a2-dashboard.nav 增加 '⬆ 版本管理'
GATE: PASS
```

## 5. 未完成项、限制与风险

- **客户端检查更新接口未做**：服务端有版本数据与灰度策略，但客户端没有更新流程，数据暂不会被真实消费。
- 白名单灰度未做；覆盖率是策略值，服务端不计算实际命中比例。
- 页面为单页列表（limit=50），未接分页控件。
- 视觉基线本次**有意**变更（导航多一项），新基线：`docs/impl/evidence/ADM-UI-09d/screens/manifest.json`。

## 6. 回滚

`git checkout --` routes/api/App/tests；删除 `scripts/admin-ui-e2e-versions.py` 与 `docs/impl/evidence/ADM-UI-09d/`；矩阵 A11 回退为 blocked-by-api。
