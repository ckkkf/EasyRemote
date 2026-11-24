# ADM-UI-08a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-08a.md`
> 时间：2026-11-23 11:36-11:46（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/features/config/diff.ts` | diff/校验/保存门控的纯函数（changedFields/invalidFields/canSave） |
| `web-admin/src/features/audit/filters.ts` | 动作精确筛选归一 + 本页动作仅作提示 |
| `web-admin/src/App.tsx` | 配置中心加「待提交变更」diff 卡片、保存门控统一、历史/回滚禁用；审计动作改输入框（datalist 提示）、导出禁用、表格改 DataTable |
| `web-admin/tests/config-audit.test.ts` | 5 条断言 |
| `scripts/admin-ui-e2e-config-audit.py` | 13 条真实 E2E |
| `docs/impl/tasks/ADM-API-05-config-audit-contract.md` | 配置历史/回滚/审计选项/导出的 contract-first 卡 |

## 2. 字段映射

| 展示项 | 来源 |
|---|---|
| 配置项/当前值/默认/范围/说明 | `ConfigView.fields` |
| diff | 前端对比 `draft` 与 `ConfigView.config` |
| 变更历史/回滚/分组/作用域 | 无接口 → 禁用 + 说明（ADM-API-05） |
| 审计列 | `AuditRow` |
| 动作筛选 | 服务端精确等值 `action`；合法集合需 ADM-API-05 |
| 审计导出 | 无接口 → 禁用（ADM-API-05） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| diff 只报真变化 | `TestChangedFieldsOnlyReportsRealChanges` | PASS |
| 非法项按契约 kind 判定 | `TestInvalidFieldsFollowsContractKind` | PASS |
| 保存门控四类阻断 | `TestCanSaveGatesOnPermissionChangesAndValidity` | PASS |
| 动作精确匹配 / 本页仅提示 | `TestAuditActionFilterIsExactMatch` / `TestSeenActionsIsHintOnly` | PASS |
| 配置 diff + 保存审计 + 审计筛选 | `TestConfigAuditE2E` | PASS 13/13 |
| 六屏视觉判据 | `TestVisualBaselineUnchanged` | PASS（与 07 基线一致） |
| 回归/技术栈/闸门 | `npm test`（43）/ `stack-check` 4/4 / `gate.sh` | 全绿 |

## 4. 关键输出

```text
Tests  43 passed (43)
PASS diff lists before -> after -> session.max_minutes: 720 -> 601
PASS config save writes audit -> config.update {"session.max_minutes":"720"}->{"session.max_minutes":"601"}
PASS invalid value disables save
PASS action filter is text input
PASS exact action filter -> ['user.ban']
E2E OK (13 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

截图：`config-diff.png`、`audit-filter.png`。

## 5. 未完成项、限制与风险

- 变更历史、一键回滚、配置分组/作用域、审计合法动作集合、时间范围/操作人/对象筛选、CSV 导出均依赖 **ADM-API-05**；本卡只做禁用与说明，**不用本地状态冒充服务端回滚**。
- 配置表格仍是"表单表格"（含输入控件），未套 DataTable；这是有意例外，因为 DataTable 面向只读行渲染。若后续要统一，需要 DataTable 支持可编辑列。
- 审计动作筛选从下拉改输入框：服务端是精确等值匹配，输入需要完整动作名；datalist 只是本页提示，不代表全集。

## 6. 回滚

删除 `features/config/diff.ts`、`features/audit/filters.ts`、`tests/config-audit.test.ts`、`scripts/admin-ui-e2e-config-audit.py`、`docs/impl/evidence/ADM-UI-08/`；`git checkout -- web-admin/src/App.tsx`。
