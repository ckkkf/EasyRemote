# ADM-UI-07b2 验证记录（双人确认中断页面）

> 卡：`docs/impl/tasks/ADM-UI-07b2-kill-double-confirm-ui.md`
> 时间：2026-11-23 15:17-15:26（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/api.ts` | `killRequest` / `killConfirm` / `killRequests` + `KillRequestRow` |
| `web-admin/src/App.tsx` | 行内「双人中断」+ 待确认卡片 + 确认对话框 |
| `server/internal/stack/stack_pg.go`、`seed_test.go` | 开发环境 seed 可登录的第二操作人 `ops2-dev`（active + TOTP），生产不 seed |
| `scripts/admin-ui-e2e-sessions.py` | 28 条 E2E（含双人确认全链路） |
| `page-matrix.md` | A9 描述更新 |

## 2. 交互链路

发起（第一人）→ 待确认卡片 → 第一人点确认被拒（`ADMIN_FORBIDDEN`）→ 第二人确认 → 会话 closed + 卡片消失 + `session.kill_confirm` 审计。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 开发 seed（含生产不 seed） | `TestDevSeedsDevAdmin`、`TestProductionNeverSeedsDevAdmin` | PASS |
| 后端双人链路（回归） | `TestKillRequestTwoPersonFlow`、`TestKillRequestExpires` | PASS |
| 端到端 | `TestSessionsE2E` | PASS 28/28 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
PASS dual request requires reason
PASS pending request listed -> ses-...
PASS dual request writes audit
PASS requester cannot confirm own request -> 700005 ADMIN_FORBIDDEN
PASS second admin confirmed and closed session
PASS session closed after confirmation
PASS confirm writes audit
E2E OK (28 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

新增「双人中断」按钮后，既有 E2E 的 `has_text="中断"` 同时命中「双人中断」与「中断」（strict mode 报错）。已改为 `get_by_role("button", name="中断", exact=True)`，既有断言语义不变、不再歧义。

## 6. 未完成项、限制与风险

- 待确认请求没有"通知第二人"的能力（需要站内信/IM）；目前靠第二人主动刷新页面。
- 页面只显示 pending；confirmed/expired 历史需查审计（`session.kill_confirm` / `session.kill_request`）。
- 过期清理仍无后台定时任务（ADM-API-07b1 已记录）。
- 开发环境多了一个可登录的 `ops2-dev`（共享开发 TOTP）；生产被 `IsProduction` 拦住并有测试守护。

## 7. 回滚

`git checkout --` api/App/stack seed/E2E；删除 `docs/impl/evidence/ADM-UI-07b2/`；矩阵 A9 描述回退。
