# ADM-API-03e 验证记录（用户时间范围 + CSV 导出）

> 卡：`docs/impl/tasks/ADM-API-03e-users-export.md`
> 时间：2026-11-23 16:05-16:16（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `store/{memory,pg}.go` | 用户列表/计数按 `created_at` 过滤（List 与 Count 同条件） |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `CsvExportResult` + ADM-40 |
| `adminapi/adminapi.go`、`users_export_test.go` | `ExportUsers`（脱敏 + 审计 + 截断） |
| `adminhttp/handler.go` | 导出路由 |
| `web-admin/src/api.ts`、`App.tsx` | 注册时间范围输入 + 真实导出下载 |
| `scripts/admin-ui-e2e-users.py` | 26 条 E2E |
| `page-matrix.md` | A3 / A4 → implemented |

## 2. 字段映射

| 端点/参数 | 说明 |
|---|---|
| `GET /admin/v1/users?from=&to=` | 注册时间范围（RFC3339） |
| ADM-40 `GET /admin/v1/users/export` | `CsvExportResult{csv, rows, truncated}`，上限 5000 |
| 审计 | `user.export`，after=`rows=N;truncated=bool` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（68 端点） |
| 时间范围/脱敏/审计/截断 | `TestUsersTimeRangeAndExport` | PASS |
| 端到端 | `TestUsersE2E` | PASS 26/26 |
| 六屏判据 | `TestVisualBaselineUnchanged` | 仅 `a3-users.filters` 2→4（预期），新 manifest 为基线 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（68 个类型 / 68 条接口）
✓ 路由 method/path/auth 与契约一致（68 条端点）
--- PASS: TestUsersTimeRangeAndExport
PASS future range returns no users
PASS clearing range restores rows
PASS csv masks phone -> ...139****5000...
PASS export writes audit
E2E OK (26 checks)
VISUAL DIFF: a3-users.filters 2 -> 4（预期）
GATE: PASS
```

## 5. 过程中修掉的问题

1. 测试首次因 `CreateAccount` 唯一键冲突失败——夹具账号缺 `PhoneHash`；补齐后通过。
2. 断言"时间范围内只有新账号"失败——`setup()` 自己也 seed 了账号；改为断言"窗口外的被排除、窗口内的保留"，不假设总量。
3. E2E 首版把掩码写成 `138****5000`（实际号码是 139 开头），断言失败但功能正确；已按真实号码修正。

## 6. 未完成项、限制与风险

- 导出上限 5000 行；超大导出需要流式接口（会破坏 JSON 信封约定，需负责人批准例外）。
- 导出字段不含备注/设备码明细（只给设备数），需要时再扩展列。
- 时间范围只有注册时间；"最近登录时间范围"未做。
- 视觉基线本次**有意**变更（新增两个时间输入），新基线：`docs/impl/evidence/ADM-API-03e/screens/manifest.json`。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 `users_export_test.go` 与 `docs/impl/evidence/ADM-API-03e/`。
