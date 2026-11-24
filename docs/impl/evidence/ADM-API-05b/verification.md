# ADM-API-05b 验证记录（配置历史 + 回滚）

> 卡：`docs/impl/tasks/ADM-API-05b-config-history.md`
> 时间：2026-11-23 12:48-12:58（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | ADM-15/16 + `ConfigHistoryEntry`/`ConfigHistoryResult` |
| 三端生成物、`route_specs.go` | 生成 + 声明 |
| `server/internal/adminapi/config.go` | `ConfigHistory`（读审计 config.*）、`RollbackConfig`（单键回滚 + 审计） |
| `config_history_test.go` | 断言历史/权限/回滚/未知 key |
| `server/internal/adminhttp/handler.go` | 两条路由 |
| `web-admin/src/api.ts`、`App.tsx` | 历史卡片 + 「回滚到旧值」 |
| `scripts/admin-ui-e2e-config-audit.py` | 16 条 E2E |
| `web-admin/tests/admin-scope.test.ts` | 白名单同步 |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-15 `/admin/v1/config/history` | — | `ConfigHistoryResult{items}`（before/after JSON、actor、ts、result） |
| ADM-16 `/admin/v1/config/rollback` | `{key, value}` | `ConfigPutResult` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（43 端点） |
| 历史内容/权限/回滚/未知 key | `TestConfigHistoryAndRollback` | PASS |
| 端到端历史与回滚 | `TestConfigAuditE2E` | PASS 16/16 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（43 个类型 / 43 条接口）
✓ 路由 method/path/auth 与契约一致（43 条端点）
--- PASS: TestConfigHistoryAndRollback (0.00s)
PASS rollback restores previous value -> 720
E2E OK (16 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 未完成项、限制与风险

- 历史从审计读取（最近 200 条内过滤 config.*），不是独立版本表；量很大时需要专门索引/表。
- 回滚按**单键**执行；批量回滚需要多次点击（UI 有意如此，避免一键改多个键难审计）。
- 历史条目的 before/after 是 JSON 字符串，UI 解析失败时显示「无可解析的键值」，不崩。
- 配置分组/作用域、审计 CSV 导出仍缺。

## 6. 回滚

`git checkout --` 契约/生成物/route_specs/config.go/handler/前端/E2E/admin-scope；删除 `config_history_test.go` 与 `docs/impl/evidence/ADM-API-05b/`。
