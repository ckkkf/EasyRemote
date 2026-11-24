# ADM-API-03g 验证记录（会话详情：双方设备 / 链路切换 / 状态时间线）

> 卡：`docs/impl/tasks/ADM-API-03g-session-detail.md`
> 时间：2026-11-23 18:44-18:56（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `SessionParty`/`LinkSwitch`/`TimelineEntry`/`SessionDetailResult` + ADM-41 |
| `adminapi/adminapi.go`、`session_detail_test.go` | `SessionDetail`（双方设备、链路切换、时间线） |
| `adminhttp/handler.go` | 路由 |
| `web-admin/src/api.ts`、`features/sessions/session-detail-page.tsx` | 三个新区块（双方设备 / 链路切换 / 状态时间线） |
| `scripts/admin-ui-e2e-sessions.py` | 36 条 E2E |

## 2. 语义

| 字段 | 来源 |
|---|---|
| `controller/target` | devices 表（设备码/平台/系统/客户端/在线） |
| `link_switches` | 采样按时间排序后检测 link 变化（忽略未上报） |
| `timeline` | 创建（started_at）+ 结束（ended_at + reason） |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（70 端点） |
| 双方设备/切换/时间线 | `TestSessionDetailPartiesAndSwitches` | PASS |
| 端到端 | `TestSessionsE2E` | PASS 36/36 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（72 个类型 / 70 条接口）
✓ 路由 method/path/auth 与契约一致（70 条端点）
--- PASS: TestSessionDetailPartiesAndSwitches
PASS session parties listed -> 2
PASS link switch recorded -> ... p2p -> relay
PASS timeline rendered
E2E OK (36 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

1. `go-envelope` 红：GET+admin 恒为 200 的约定被空 `session_id` 打破；改为返回空结构（与 ADM-17 处理一致）。
2. `admin-scope` 白名单顺序错位（`sessions/detail` 应排在 `sessions/kill` 前），排序后通过。

## 6. 未完成项、限制与风险

- **错误码时间线仍缺** → `ADM-API-03h`（需要会话级错误事件存储，不能拼假数据）。A8 因此保持 partial。
- 时间线只有"创建/结束"两个点，没有中间状态迁移（authorized/active 时间未单独记录）。
- 链路切换基于采样，采样间隔内的切换会漏掉。

## 7. 回滚

`git checkout --` 契约/生成物/adminapi/handler/前端/E2E；删除 `session_detail_test.go` 与 `docs/impl/evidence/ADM-API-03g/`。
