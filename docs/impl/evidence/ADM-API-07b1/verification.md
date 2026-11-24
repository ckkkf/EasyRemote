# ADM-API-07b1 验证记录（双人确认中断）

> 卡：`docs/impl/tasks/ADM-API-07b1-kill-request.md`
> 时间：2026-11-23 15:09-15:17（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/010_kill_requests.sql` | `kill_requests` 表（status + expires_at） |
| `store/{store,memory,pg}.go` | 创建/读取/列表/确认（CAS）/过期 |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `KillRequestRow`/`KillRequestCreate`/`KillConfirmRequest` + ADM-31/32/33 |
| `adminapi/adminapi.go`、`kill_request_test.go` | `RequestKill`/`ConfirmKill`/`ListKillRequests` |
| `adminhttp/handler.go` | 三条路由 |
| `web-admin/tests/admin-scope.test.ts` | 白名单同步 |

## 2. 状态机

`pending --(第二人确认, 未过期)--> confirmed --(session.Kill)--> 会话 closed`
`pending --(过期)--> expired`（`ExpireKillRequests` 可批量收敛）

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（61 端点） |
| 双人链路 + 同人拒绝 + 重复确认 | `TestKillRequestTwoPersonFlow` | PASS |
| TTL 过期 | `TestKillRequestExpires` | PASS |
| 回归（store/adminapi/adminhttp/web 56） | `go test` / `npm test` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（62 个类型 / 61 条接口）
✓ 路由 method/path/auth 与契约一致（61 条端点）
--- PASS: TestKillRequestTwoPersonFlow
--- PASS: TestKillRequestExpires
GATE: PASS
```

## 5. 未完成项、限制与风险

- **页面未做**（ADM-UI-07b2）：现在只有 API，管理端仍走单次中断；双人确认没有入口。
- 过期清理是"确认时校验 + 可选批量 Expire"，没有后台定时任务；`pending` 会一直留到被确认或批量收敛。
- 权限沿用 `session.kill`；若将来需要"只有超管能确认"，需要单独权限动作。
- 通知（让第二人知道有待确认）没有做，需要站内信/IM 集成。

## 6. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 010 迁移、`kill_request_test.go` 与 `docs/impl/evidence/ADM-API-07b1/`。
