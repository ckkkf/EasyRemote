# ADM-API-05c1 验证记录（配置分组 + 平台作用域）

> 卡：`docs/impl/tasks/ADM-API-05c1-config-scope.md`
> 时间：2026-11-23 14:49-14:58（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物 | `ConfigField.group`、`ConfigView.platform`、`ConfigPutRequest{values, platform}` |
| `server/internal/stack/migrations/009_config_overrides.sql` | `config_overrides(key, platform, value)` |
| `store/{store,memory,pg}.go` | `ListConfigOverrides` / `SetConfigOverride`（platform="" 走全局表） |
| `appconfig/appconfig.go`、`scoped_test.go` | `Group` 元数据、`AllFor`/`UpdateFor`、平台白名单 |
| `adminapi/config.go`、`config_scoped_test.go` | `GetConfigFor`/`PutConfigFor`（审计对象带作用域） |
| `adminhttp/handler.go` | GET 读 `platform` 查询参数；PUT 解 `ConfigPutRequest` |
| `web-admin` 两文件 | 作用域选择器 + 分组前缀 + 保存带 platform |
| `scripts/admin-ui-e2e-config-audit.py` | 20 条 E2E（含作用域隔离） |

## 2. 读取优先级

`默认值 ← 全局覆盖（app_config） ← 平台覆盖（config_overrides）`

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（58 类型 / 57 接口） |
| 优先级与校验 | `TestScopedConfigPrecedence` | PASS |
| 作用域审计与隔离 | `TestScopedConfigAudit` | PASS |
| 端到端作用域隔离 | `TestConfigAuditE2E` | PASS 20/20 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（58 个类型 / 57 条接口）
✓ 路由 method/path/auth 与契约一致（57 条端点）
--- PASS: TestScopedConfigPrecedence
--- PASS: TestScopedConfigAudit
PASS config group shown
PASS scoped save audits with platform -> app_config@windows
PASS global unchanged after platform save
PASS platform scope keeps its own value
E2E OK (20 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

- `All()` 改造时一度写出重复的 `mergeDefaults`（残留旧函数体导致编译错），已重写为单一实现，并把 `All()` 明确为"全局作用域的 AllFor"。

## 6. 未完成项、限制与风险

- **版本作用域**未做（A12 spec 要求"平台/版本作用域"），需要版本维度与优先级规则。
- 分组只是元数据 + 前端前缀展示，未做按组折叠/分区布局。
- `config_overrides` 无删除语义：写空值会被当作有效覆盖（需后续"恢复默认"操作）。
- 客户端读取 `GET /v1/config` 仍是全局值，未按平台下发（需要客户端上报平台）。
- 审计 CSV 导出仍未做（同属 05c 剩余）。

## 7. 回滚

`git checkout --` 契约/生成物/store/appconfig/adminapi/handler/前端/E2E；删除 009 迁移与 `docs/impl/evidence/ADM-API-05c1/`。
