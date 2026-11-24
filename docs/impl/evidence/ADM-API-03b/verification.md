# ADM-API-03b 验证记录（nickname / last_login_at）

> 卡：`docs/impl/tasks/ADM-API-03b-account-profile.md`
> 时间：2026-11-23 12:14-12:24（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `migrations/003_account_profile.sql` | 幂等加列 `nickname`、`last_login_at` |
| `server/internal/stack/stack_pg.go` | `LoadMigration` 改为按文件名排序拼接全部 `migrations/*.sql`（此前只读 001） |
| `server/internal/store/{store,memory,pg}.go` | Account 加两字段；`TouchAccountLogin`；PG 查询列表与扫描同步 |
| `storetest/storetest.go` | 契约用例 `AccountLastLogin` |
| `server/internal/auth/auth.go` | 登录成功写最近登录（失败按存储错误返回） |
| `server/internal/adminapi/adminapi.go` | ListUsers 输出 nickname / last_login_at（零值保持 0001，UI 显示「—」） |
| `web-admin` 三文件 | 类型 + 列表「昵称」「最近登录」列 + 详情 |
| `scripts/admin-ui-e2e-users.py` | 8 列 + 昵称/最近登录断言 |

## 2. 字段映射

| 字段 | 类型 | 语义 |
|---|---|---|
| `nickname` | string | 运营设置的昵称；空串表示未设置（无编辑接口，不造假名） |
| `last_login_at` | time? | 最近一次成功登录；历史账号 0001-01-01 → UI 显示「—」 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三端契约一致 | `gen-contract --check` | PASS |
| 写入最近登录 / 未知账号 ErrNotFound | `TestMemoryContract/AccountLastLogin` | PASS |
| 登录写最近登录 | `TestLoginRecordsLastLoginAt` | PASS |
| 列表 8 列 + 昵称「—」+ 最近登录「刚刚」+ 脱敏 | `TestUsersE2E` | PASS 15/15 |
| 视觉判据 | `TestVisualBaselineUnchanged` | 仅 a3 列变化（有意） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
--- PASS: TestMemoryContract/AccountLastLogin (0.00s)
--- PASS: TestLoginRecordsLastLoginAt (0.00s)
PASS users table columns -> ['账号 ID','昵称','手机号','设备数','状态','注册时间','最近登录','操作']
PASS nickname empty until set -> ['—','—']
PASS last login recorded -> ['刚刚','刚刚']
E2E OK (15 checks)
a3-users.columns: [6 列] -> [8 列]   # 有意变化
GATE: PASS
```

## 5. 未完成项、限制与风险

- **昵称无法编辑**：没有资料编辑接口，列恒为「—」；要有真实昵称需另开写接口卡（并写审计）。
- 迁移改成"拼接全部 SQL"后，任何新迁移必须幂等（已在注释里写明）；PG 契约测试仍只读 001，属既有测试的局限。
- `last_login_at` 用 `time?` 约定（与 `SessionRow.ended_at` 一致），零值序列化为 `0001-01-01`，UI 统一显示「—」。
- 回滚：`git checkout --` 契约/生成物/store/auth/adminapi/前端/E2E；删除 003 迁移与证据目录。

## 6. 回滚

见卡内回滚命令。
