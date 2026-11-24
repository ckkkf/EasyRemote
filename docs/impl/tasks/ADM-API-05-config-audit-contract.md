# ADM-API-05 配置历史/回滚与审计选项/导出契约

> 来源：ADM-UI-08a 执行时确认的缺口；`docs/spec/pages.md` A12（行 256-260）、A13（行 262-266）、PRD §6.3.7（行 626-637）、§6.3.8（行 639-643）
> 性质：**contract-first 后端卡**。

## 缺口

| 需求 | 现状 | 需要的契约变化 |
|---|---|---|
| 配置变更历史 | 无接口 | `GET /admin/v1/config/history?key=&limit=`（读审计或独立版本表） |
| 一键回滚 | 无接口 | `POST /admin/v1/config/rollback`（目标版本 + 原因 + 审计） |
| 配置分组/作用域 | `ConfigField` 无 group/scope | `ConfigField` 增 `group`、`scope`（全局/平台/版本） |
| 审计合法动作集合 | 无接口（前端只能猜） | `GET /admin/v1/audit/actions` 返回合法 action 列表 |
| 审计时间范围/操作人/对象筛选 | 只有 `q`/`action`/`result` | `GET /admin/v1/audit` 增 `from`/`to`/`actor`/`object`（服务端过滤） |
| 审计 CSV 导出 | 无接口 | `GET /admin/v1/audit/export`（服务端生成 + 审计 `audit.export`） |

## 必须通过的断言（执行时）

- [ ] `TestConfigHistoryReturnsBeforeAfter`：历史项含 before/after/actor/ts。
- [ ] `TestConfigRollbackWritesAuditAndRestores`：回滚后配置值恢复且审计含 `config.rollback`。
- [ ] `TestAuditActionsEndpointListsKnownActions`：返回集合包含 `user.ban`/`session.kill`/`config.update`。
- [ ] `TestAuditExportWritesAudit`：导出写审计，且不含明文手机号。

## 状态

**未开始**。ADM-UI-08a 已把入口做成禁用 + 说明，审计动作筛选改为"精确输入 + 本页提示"，不再用当前页猜下拉。

## 进度

- 2026-11-23（ADM-API-05a）：审计动作集合接口、actor/object/时间范围筛选、Count/List 条件统一已落地。
- 仍缺：配置变更历史、一键回滚、配置分组/作用域、审计 CSV 导出。

## 进度（续）

- 2026-11-23（ADM-API-05b）：配置变更历史（读审计）与单键一键回滚已落地（`config.rollback` 审计）。
- 仍缺：配置分组/作用域字段、审计 CSV 导出。
