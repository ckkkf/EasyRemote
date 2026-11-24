# ADM-API-05c2 验证记录（审计 CSV 导出）

> 卡：`docs/impl/tasks/ADM-API-05c2-audit-export.md`
> 时间：2026-11-23 14:58-15:08（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | ADM-30 `GET /admin/v1/audit/export` + `AuditExportResult{csv, rows, truncated}` |
| `adminapi/adminapi.go`、`export_test.go` | `ExportAudit`：服务端生成 CSV、脱敏、转义、写 `audit.export` |
| `adminhttp/handler.go` | 路由（沿用审计筛选参数） |
| `web-admin/src/api.ts`、`App.tsx` | `exportAudit()` + 导出按钮（Blob 下载 + 行数/截断提示） |
| `scripts/admin-ui-e2e-config-audit.py` | 24 条 E2E（含真实下载） |

## 2. 脱敏与转义

| 项 | 规则 |
|---|---|
| IPv4 | `203.0.113.42 → 203.0.x.x` |
| IPv4:port | `127.0.0.1:54321 → 127.0.x.x:54321` |
| IPv6 | `2001:db8::1 → 2001:****` |
| 逗号/引号/换行 | RFC4180 包裹 + 引号翻倍 |
| 上限 | 5000 行，命中即 `truncated=true` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（58 端点） |
| 脱敏/转义/截断/审计/权限 | `TestExportAuditCSV` | PASS |
| 端到端下载 | `TestConfigAuditE2E` | PASS 24/24 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（59 个类型 / 58 条接口）
✓ 路由 method/path/auth 与契约一致（58 条端点）
--- PASS: TestExportAuditCSV
PASS audit export enabled
PASS csv header exported -> 时间,操作人,角色,动作,对象,变更前,变更后,IP,结果
PASS csv masks ip -> ...127.0.x.x:53299,ok
PASS export count shown
PASS export writes audit
E2E OK (24 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

首次 E2E 失败：审计里的 IP 是 `RemoteAddr` 原样（`127.0.0.1:53299`），旧 `maskIP` 看到冒号就当 IPv6，脱敏成 `127.0.0.1:****`（端口泄露 + IPv4 段未脱敏）。已改为先 `net.SplitHostPort` 拆端口再按 IPv4 脱敏，并补了单测。

## 6. 未完成项、限制与风险

- 导出走 JSON 信封（`csv` 字段），5000 行上限；超大导出需要流式接口（会破坏信封约定，需负责人批准例外）。
- 用户/设备/会话模块的导出未做（A3 的 CSV 导出仍禁用）。
- 脱敏只覆盖 IP；若将来审计对象里出现手机号/设备码，需要扩展脱敏规则。
- 导出内容包含 `before/after` JSON，可能含配置值但不含密钥（配置项白名单内）。

## 7. 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E；删除 `export_test.go` 与 `docs/impl/evidence/ADM-API-05c2/`。
