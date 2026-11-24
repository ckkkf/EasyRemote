# ADM-API-05d 配置版本作用域

> 来源：`docs/impl/tasks/ADM-API-05c-config-scope-export.md`（剩余：版本作用域）
> 需求：`docs/spec/pages.md` A12（行 258「平台/版本作用域」）、PRD §6.3.7
> 前置：ADM-API-05c1（平台作用域）

## 目标

在平台作用域之上再加一层**版本作用域**：

1. `config_overrides` 增 `version` 列，主键 (key, platform, version)；
2. 读取优先级：默认值 ← 全局 ← 平台（version 空） ← 平台+版本；
3. 审计对象格式：全局 `app_config@`、平台 `app_config@windows`、平台+版本 `app_config@windows@0.2.13`；
4. 版本作用域必须带平台（否则 `ADMIN_CONFIG_INVALID`）；
5. 前端作用域选择器旁加版本输入框。

## 必须通过的断言

- [ ] `TestScopedConfigPrecedence`：windows@0.2.13=120、windows@0.2.12 回落 300、无平台带版本被拒。
- [ ] `TestConfigAuditE2E`：版本作用域保存后审计 object=`app_config@windows@0.2.13`；其它版本回落平台值；平台作用域审计仍是 `app_config@windows`。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/stack/migrations/017_config_version_scope.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/appconfig/{appconfig,scoped_test}.go`
- `server/internal/adminapi/config.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-config-audit.py`
- `docs/impl/tasks/ADM-API-05d-config-version-scope.md`、`docs/impl/evidence/ADM-API-05d/**`

禁止：版本作用域覆盖全局；审计对象丢版本信息；前端本地过滤冒充作用域。

## 验收命令

```bash
cd server && go test ./internal/appconfig/ -run 'ScopedConfig' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-config-audit.py
bash scripts/gate.sh
```

## 完成标准

三条断言全绿；E2E `E2E OK (26 checks)`；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/appconfig/adminapi/handler/前端/E2E；删除 017 迁移与 `docs/impl/evidence/ADM-API-05d/`。
