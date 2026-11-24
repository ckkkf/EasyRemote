# ADM-API-03d 验证记录（解封 / 踢下线）

> 卡：`docs/impl/tasks/ADM-API-03d-user-actions.md`
> 时间：2026-11-23 15:58-16:08（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `UserActionRequest` + ADM-38/39 |
| `adminapi/adminapi.go`、`user_actions_test.go` | `UnbanUser` / `KickUser`（原因必填 + 审计 + 作用域隔离） |
| `adminhttp/handler.go` | 两条路由（共用 userAction 包装） |
| `web-admin/src/api.ts`、`App.tsx` | 行内「解封 / 踢下线」+ 原因对话框 |
| `scripts/admin-ui-e2e-users.py` | 22 条 E2E |
| `web-admin/tests/admin-scope.test.ts` | 白名单同步 |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-38 `/users/unban` | `{account_id, reason}` | `OkResult` |
| ADM-39 `/users/kick` | `{account_id, reason}` | `OkResult`（审计含 closed/notify_failed/offline 计数） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（67 端点） |
| 解封/踢下线/作用域/审计/权限 | `TestUnbanAndKickUser` | PASS |
| 端到端 | `TestUsersE2E` | PASS 22/22 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（67 个类型 / 67 条接口）
✓ 路由 method/path/auth 与契约一致（67 条端点）
--- PASS: TestUnbanAndKickUser
PASS kick requires reason
PASS kick writes audit
PASS ban applied
PASS unban writes audit
E2E OK (22 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 未完成项、限制与风险

- **时间范围筛选**与**用户列表 CSV 导出**仍未做（A3 最后两项）。
- 踢下线只关闭会话 + 置离线，不撤销设备凭证/信任关系（设备可立即重连）；彻底封禁需用封禁账号。
- 解封不恢复历史会话，只改账号状态。
- 踢下线失败（通知器不可用）会记在审计 `notify_failed`，但接口仍返回成功——因为会话确实已关闭；前端提示未区分该计数。

## 6. 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E/admin-scope；删除 `user_actions_test.go` 与 `docs/impl/evidence/ADM-API-03d/`。
