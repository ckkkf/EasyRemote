# ADM-API-05a 审计服务端筛选 + 合法动作集合

> 来源：`docs/impl/tasks/ADM-API-05-config-audit-contract.md`（审计动作集合、时间范围/操作人/对象筛选）
> 需求：`docs/spec/pages.md` A13（行 264-265）、PRD §6.3.8（行 639-643）
> 前置：ADM-UI-08a（审计页已把动作筛选改成输入框并标注缺口）

## 目标

1. 新增 `GET /admin/v1/audit/actions` 返回服务端合法动作集合，审计页下拉用它，不再从当前页数据猜；
2. `GET /admin/v1/audit` 支持 `actor`/`object` 精确过滤与 `from`/`to` 时间范围（RFC3339），服务端分页；
3. 修掉 PG `CountAudit` 只认 `q` 的缺陷（分页总数与列表过滤条件不一致）。

## 输入 / 输出

| 端点/参数 | 说明 |
|---|---|
| `GET /admin/v1/audit/actions` | `AuditActionsResult{actions: list<string>}`，去重排序 |
| `GET /admin/v1/audit?actor=&object=&from=&to=` | 精确/范围过滤；坏时间格式按"不限"处理，不清空整页 |
| `data.total` | 与过滤条件一致（List/Count 共用同一 where 构造） |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：41 条端点一致。
- [ ] `TestMemoryContract/AuditFiltersAndActions`：actor/object/时间范围过滤正确；actions 去重且排序。
- [ ] `TestAuditActionsReturnsServerSet`：返回集合正确；readonly 可读；未知角色 `ADMIN_FORBIDDEN`。
- [ ] `TestConfigAuditE2E`：动作下拉选项来自服务端（含 user.ban/config.update）、精确筛选、操作人过滤、未来时间范围 0 行、清空恢复。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/store/{store,memory,pg}.go`、`storetest/storetest.go`
- `server/internal/adminapi/adminapi.go`、`adminapi_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`features/audit/filters.ts`、`tests/config-audit.test.ts`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-config-audit.py`
- `docs/impl/tasks/ADM-API-05a-audit-filters.md`、`docs/impl/evidence/ADM-API-05a/**`

禁止：手改生成物；用当前页数据构造动作选项；前端切片冒充时间范围过滤。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ -run 'Contract|AuditActions' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-config-audit.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (15 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E/admin-scope；删除 `docs/impl/evidence/ADM-API-05a/`。
