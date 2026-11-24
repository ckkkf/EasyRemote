# ADM-API-05c1 配置分组 + 平台作用域

> 来源：`docs/impl/tasks/ADM-API-05c-config-scope-export.md`（拆分子卡 1）
> 需求：`docs/spec/pages.md` A12（行 256-260）、PRD §6.3.7（行 626-637）
> 前置：ADM-API-05a/05b（审计筛选、变更历史与回滚）

## 目标

1. 配置项增加**分组**元数据（画质 / 连接 / 限流 / 开关），管理端按组显示；
2. 增加**平台作用域**：`platform=""` 为全局，其余为 windows/macos/android/ios；
3. 读取优先级：**默认值 ← 全局覆盖 ← 平台覆盖**；写平台作用域不影响全局；
4. 审计对象带作用域（`app_config@windows`），避免"改了哪个平台"说不清。

## 输入 / 输出

| 端点 | 变化 |
|---|---|
| `GET /admin/v1/config?platform=` | 返回该作用域的生效值；`ConfigView.platform` 回显 |
| `PUT /admin/v1/config` | 请求体新增 `platform`（`ConfigPutRequest`） |
| `ConfigField` | 新增 `group` |

## 必须通过的断言

- [ ] `gen-contract --check`：三端一致（58 类型 / 57 接口）。
- [ ] `TestScopedConfigPrecedence`：默认 720 → 全局 600 → windows 300；macos 继承 600；非法平台被拒；作用域写入走范围校验。
- [ ] `TestScopedConfigAudit`：`app_config@windows` 审计；平台值不污染全局；非法平台 `ADMIN_CONFIG_INVALID`。
- [ ] `TestConfigAuditE2E`：页面显示「连接 · session.max_minutes」；切到 windows 改 300 保存后审计 object=`app_config@windows`；切回全局仍 720；再切 windows 仍是 300。
- [ ] 六屏判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `server/internal/stack/migrations/009_config_overrides.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`
- `server/internal/appconfig/{appconfig,scoped_test}.go`
- `server/internal/adminapi/{config,config_scoped_test}.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`
- `scripts/admin-ui-e2e-config-audit.py`
- `docs/impl/tasks/ADM-API-05c1-config-scope.md`、`docs/impl/evidence/ADM-API-05c1/**`

禁止：平台作用域写入全局表；用前端过滤假装作用域；改审计对象格式但不带作用域。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/appconfig/ ./internal/adminapi/ -run 'ScopedConfig' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-config-audit.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (20 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/appconfig/adminapi/handler/前端/E2E；删除 009 迁移与 `docs/impl/evidence/ADM-API-05c1/`。
