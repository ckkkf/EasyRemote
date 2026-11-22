# ER-801 证据：分页下沉（部分）

- 状态: 部分完成（用户页服务端分页；total 推导）
- gate: PASS

## 1. 变更

| 层 | 内容 |
|---|---|
| store.Page | Query/Sort/Desc/Status |
| Memory | 账号/设备/会话过滤+排序 |
| PG | ILIKE/status 过滤 + 白名单排序 |
| adminapi | ListUsers/Devices/Sessions 接收 Page |
| adminhttp | parsePage（limit/offset/q/sort/desc/status） |
| web-admin | api ListQuery；用户页服务端分页，移除客户端 filter/slice |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 账号 query/status/sort | TestPageQuerySortMemory | PASS |
| 设备 query/sort | 同上 | PASS |
| 会话 query | 同上 | PASS |
| 用户页不再客户端分页 | 代码审查 + tsc | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- httpx.Page.total 仍是当前页条数；用户页用 offset+len 推导，不等于真实总数。
- 设备/会话/审计页面前端仍保留部分客户端过滤；需同样改为服务端参数。
- PG 过滤未在真实 PG 集成测试验证。
- 审计列表仍只有 limit，无 offset/q/sort。

## 4. 下一步

ER-801a：Store Count + 真实 total；设备/会话/审计页统一服务端分页；PG smoke。
