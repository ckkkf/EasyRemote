# ADM-API-05a 验证记录（审计筛选 + 合法动作集合）

> 卡：`docs/impl/tasks/ADM-API-05a-audit-filters.md`
> 时间：2026-11-23 12:40-12:52（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | 新增 `ADM-14 GET /admin/v1/audit/actions` + `AuditActionsResult` |
| 三端生成物、`route_specs.go` | 生成 + 声明 |
| `store/{store,memory,pg}.go` | `Page.From/To/Actor/Object`；`ListAuditActions`；PG List/Count 共用 `auditWhere` |
| `storetest` | `AuditFiltersAndActions` |
| `adminapi.go`、测试 | `AuditActions`（audit.read） |
| `adminhttp/handler.go` | 新路由 + `actor/object/from/to` 解析（坏时间按不限） |
| `web-admin` | 动作下拉来自服务端；操作人/对象/时间范围输入 |
| `scripts/admin-ui-e2e-config-audit.py` | 15 条 E2E |

## 2. 字段映射

| 端点/参数 | 说明 |
|---|---|
| `GET /admin/v1/audit/actions` | `AuditActionsResult{actions: list<string>}` |
| `?actor=&object=&from=&to=` | 精确/范围过滤（RFC3339） |
| `data.total` | 与过滤一致（List/Count 共用 where） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（41 端点） |
| 过滤 + 动作集合 | `TestMemoryContract/AuditFiltersAndActions` | PASS |
| 动作集合权限 | `TestAuditActionsReturnsServerSet` | PASS |
| 端到端筛选 | `TestConfigAuditE2E` | PASS 15/15 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（41 个类型 / 41 条接口）
✓ 路由 method/path/auth 与契约一致（41 条端点）
--- PASS: TestMemoryContract/AuditFiltersAndActions
--- PASS: TestAuditActionsReturnsServerSet
PASS action options come from server -> ['全部动作','admin.login','config.update','user.ban']
PASS actor filter
PASS future time range returns no rows
E2E OK (15 checks)
GATE: PASS
```

## 5. 过程中修掉的真实缺陷

- **PG `CountAudit` 只认 `q`**：列表按 action/result 过滤、总数却按全表算，分页会错。已抽出 `auditWhere` 让 List/Count 共用。

## 6. 未完成项、限制与风险

- 配置变更历史、一键回滚、配置分组/作用域、审计 CSV 导出仍未做（ADM-API-05 剩余）。
- 时间范围用 RFC3339 传参，前端用 `datetime-local` 转 UTC；坏格式按"不限"处理，避免整页清空。
- 动作集合是"历史上出现过的动作"，不是"允许执行的动作"清单；后者要静态枚举，属后续收敛。

## 7. 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E/admin-scope；删除 `docs/impl/evidence/ADM-API-05a/`。
