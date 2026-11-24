# ADM-API-05d 验证记录（配置版本作用域）

> 卡：`docs/impl/tasks/ADM-API-05d-config-version-scope.md`
> 时间：2026-11-23 18:51-19:01（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `migrations/017_config_version_scope.sql` | `config_overrides.version` + 主键 (key, platform, version) |
| `store/{store,memory,pg}.go` | 覆盖读写带 version 维度 |
| `appconfig/appconfig.go`、`scoped_test.go` | `AllForScope` / `UpdateForScope`（默认←全局←平台←平台+版本） |
| `adminapi/config.go` | `GetConfigScope` / `PutConfigScope` + `configScopeObject` |
| `adminhttp/handler.go` | GET/PUT 解析 `version` |
| `web-admin` 两文件 | 版本作用域输入框 |
| `scripts/admin-ui-e2e-config-audit.py` | 26 条 E2E |

## 2. 优先级与审计对象

| 作用域 | 审计对象 |
|---|---|
| 全局 | `app_config@` |
| 平台 | `app_config@windows` |
| 平台+版本 | `app_config@windows@0.2.13` |

优先级：默认值 ← 全局 ← 平台（version 空） ← 平台+版本。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三层优先级 + 平台约束 | `TestScopedConfigPrecedence` | PASS |
| 端到端版本作用域 | `TestConfigAuditE2E` | PASS 26/26 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
--- PASS: TestScopedConfigPrecedence
PASS scoped save audits with platform -> app_config@windows
PASS version scope audited with platform and version -> app_config@windows@0.2.13
PASS other version falls back to platform scope
E2E OK (26 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

**A12 配置中心收口为 implemented**（矩阵：13 implemented / 1 partial）。

## 5. 过程中修掉的问题

审计对象初版一律 `app_config@platform@version`，把平台作用域的历史格式从 `app_config@windows` 改成了 `app_config@windows@`，既有 E2E 立刻变红；抽出 `configScopeObject` 后版本为空时保持旧格式。

## 6. 未完成项、限制与风险

- 版本作用域是"精确版本匹配"，没有"最低版本/版本区间"语义（spec 未要求，但运营可能需要）。
- 客户端 `GET /v1/config` 仍只下发全局值（未按平台/版本下发），需要客户端上报平台与版本。
- 配置项没有"恢复默认/删除覆盖"操作（写空值仍算覆盖）。
- A8 仍是唯一 partial（错误码时间线 → `ADM-API-03h`）。

## 7. 回滚

`git checkout --` 契约/生成物/store/appconfig/adminapi/handler/前端/E2E；删除 017 迁移与 `docs/impl/evidence/ADM-API-05d/`。
