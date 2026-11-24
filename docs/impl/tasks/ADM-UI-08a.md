# ADM-UI-08a 配置中心 diff + 审计动作精确筛选

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-08（行 170-176）
> 需求：`docs/spec/pages.md` A12（行 256-260）、A13（行 262-266）、PRD §6.3.7/§6.3.8
> 前置：ADM-UI-02b（三态）、05a（DataTable）

## 目标

1. 配置中心：保存前显示 **before → after diff**、非法项明确提示、保存门控统一（权限/提交中/有变更/无非法项）；变更历史与一键回滚无接口 → 禁用并注明；
2. 审计日志：**修复"动作选项来自当前页"的误导下拉**，改为精确匹配输入框（本页出现过的动作仅作 datalist 提示），CSV 导出无接口 → 禁用；表格改 DataTable。

## 输入 / 输出

不改 API 契约。字段映射：

| 展示项 | 来源 |
|---|---|
| 配置项/当前值/默认值/范围/说明 | `ConfigView.fields`（`contract.yaml`） |
| diff | 前端对比 `draft` 与 `ConfigView.config`（不新增字段） |
| 变更历史 / 回滚 / 分组 / 作用域 | 无接口 → 禁用 + 说明（ADM-API-05） |
| 审计列（时间/操作人/动作/对象/结果/IP/变更） | `AuditRow` |
| 动作筛选 | 服务端精确等值匹配 `action`；合法集合需 ADM-API-05 |
| 审计导出 | 无接口 → 禁用（ADM-API-05） |

## 必须通过的断言

- [ ] `TestChangedFieldsOnlyReportsRealChanges`：无改动返回空；改动返回 from/to。
- [ ] `TestInvalidFieldsFollowsContractKind`：空值/越界/非数字/非布尔被识别。
- [ ] `TestCanSaveGatesOnPermissionChangesAndValidity`：权限、busy、无变更、非法项四类阻断。
- [ ] `TestAuditActionFilterIsExactMatch` / `TestSeenActionsIsHintOnly`：筛选是精确匹配，本页动作只作提示。
- [ ] `TestConfigAuditE2E`（`python3 scripts/admin-ui-e2e-config-audit.py`）：13 条全 PASS —— 初始无改动、历史/回滚禁用、diff 显示 before→after、保存写审计（before/after 对账）、非法值禁用保存并提示、审计列、动作筛选是输入框、导出禁用、精确筛选只剩 user.ban、清空恢复、无 pageerror。
- [ ] `TestVisualBaselineUnchanged`：六屏判据与 ADM-UI-07 基线一致。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-08a.md`、`docs/impl/tasks/ADM-API-05-config-audit-contract.md`、`docs/impl/evidence/ADM-UI-08/**`
- `web-admin/src/features/config/diff.ts`、`web-admin/src/features/audit/filters.ts`（新增）
- `web-admin/src/App.tsx`（ConfigCenter diff 面板/按钮门控、Audit 筛选与 DataTable）
- `web-admin/tests/config-audit.test.ts`
- `scripts/admin-ui-e2e-config-audit.py`

禁止：改 `docs/spec/**`、`server/**`、`prototype/**`；禁止把"本页出现的动作"当成合法动作全集；禁止用本地状态冒充服务端回滚。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-config-audit.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-08/screens --compare-to docs/impl/evidence/ADM-UI-07/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (13 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

删除 `features/config/diff.ts`、`features/audit/filters.ts`、`tests/config-audit.test.ts`、`scripts/admin-ui-e2e-config-audit.py`、`docs/impl/evidence/ADM-UI-08/`；`git checkout -- web-admin/src/App.tsx`。
