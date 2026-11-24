# ADM-API-03c 验证记录（用户详情：会话/操作记录/备注）

> 卡：`docs/impl/tasks/ADM-API-03c-user-detail.md`
> 时间：2026-11-23 15:48-15:59（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/012_account_notes.sql` | `account_notes`（账号唯一） |
| `store/{store,memory,pg}.go` | 备注读写、按账号会话查询 |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `AccountNoteResult`/`AccountNoteSet` + ADM-35/36/37 |
| `adminapi/adminapi.go`、`user_detail_test.go` | `ListUserSessions`/`GetUserNote`/`SetUserNote` + 新权限 `user.write` |
| `adminhttp/handler.go` | 三条路由 |
| `web-admin/src/api.ts`、`user-detail-page.tsx` | 三个真实区块（最近会话/操作记录/备注） |
| `scripts/admin-ui-e2e-users.py` | 19 条 E2E（含会话与备注） |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-35 | `account_id`、`limit` | `list<SessionRow>` |
| ADM-36 | `account_id` | `AccountNoteResult{account_id, note, updated_by, updated_at?}` |
| ADM-37 | `{account_id, note}` | `OkResult` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（65 端点） |
| 会话过滤/备注/审计/权限 | `TestUserDetailSessionsAndNote` | PASS |
| 端到端 | `TestUsersE2E` | PASS 19/19 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（66 个类型 / 65 条接口）
✓ 路由 method/path/auth 与契约一致（65 条端点）
--- PASS: TestUserDetailSessionsAndNote
PASS account sessions listed -> 1
PASS session reason shown
PASS note saved
PASS note writes audit
PASS note persisted in form
E2E OK (19 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

1. `user.write` 权限动作此前不存在，导致客服写备注被拒（`ADMIN_FORBIDDEN`）；已在权限表新增 `user.write`（super/ops/cs/risk）。
2. E2E 首版断言"备注出现在 body.innerText"失败——textarea 的 value 不进 innerText；改为检查 `textarea.value`。
3. E2E seed 原以为 `/v1/devices` 返回 `device_id`，实际没有；改用 bind 响应里的 `device_id`。

## 6. 未完成项、限制与风险

- 用户列表的 CSV 导出、时间范围筛选、解封、踢下线仍未做（A3 剩余）。
- 备注只有单条纯文本（无历史版本）；改动历史需查审计 `user.note` 的 before/after。
- 会话列表最多 20 条（无分页控件），需要分页卡。
- 备注不脱敏：若运营写入手机号等敏感信息，会原样存储（需后续策略）。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 012 迁移、`user_detail_test.go` 与 `docs/impl/evidence/ADM-API-03c/`。
