# ADM-API-03h 验证记录（会话错误事件 + 错误码时间线）

> 卡：`docs/impl/tasks/ADM-API-03h-session-error-events.md`
> 时间：2026-11-23 18:58-19:08（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `migrations/018_session_errors.sql` | `session_errors` 表 |
| `store/{store,memory,pg}.go` | `RecordSessionError` / `ListSessionErrors` |
| `gateway/session_errors.go`、`handler.go` | 错误落库辅助 + `sessions.ready` / `relay.ticket` 两个真实错误点 |
| `adminapi/adminapi.go` | `SessionDetail` 增加 `error_events` |
| `docs/spec/contract.yaml`、生成物 | `SessionErrorRow` + `SessionDetailResult.error_events` |
| `web-admin` 两文件 | 「错误码时间线」表 |
| `scripts/admin-ui-e2e-sessions.py` | 38 条 E2E |

## 2. 语义

| 字段 | 说明 |
|---|---|
| `code/name` | 来自 `errcode`（与信封一致） |
| `source` | 错误发生点（`sessions.ready` / `relay.ticket`） |
| `detail` | `errcode.Error.Detail` |

写入是"尽力而为"：遥测失败**不覆盖**原始错误响应（代码注释已说明理由）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 未授权 ready → 记录错误 | `TestSessionErrorsRecorded` | PASS |
| 端到端错误时间线 | `TestSessionsE2E` | PASS 38/38 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
--- PASS: TestSessionErrorsRecorded
PASS session error timeline recorded -> 1
PASS error timeline shows code and source
E2E OK (38 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

**A8 收口为 implemented → A1-A14 全部 implemented（14/0）。**

## 5. 过程中修掉的问题

E2E 里插入"错误时间线"检查后直接导航到 closed 会话，导致后续 03g 检查在错误的页面上找「链路切换」而超时；已补一次回到 active 会话的导航。

## 6. 未完成项、限制与风险

- 只有 `sessions.ready` / `relay.ticket` 两处落库；其它入口（connect/consent/quality）尚未接入，覆盖不全。
- `session_errors` 无 TTL，长期增长。
- 错误事件没有 trace_id 关联（信封有 trace，但落库未存），排障时需要靠时间对齐。
- A7/A8 的"链路切换"依赖采样间隔，可能漏掉瞬时切换。

## 7. 回滚

`git checkout --` store/gateway/adminapi/契约/生成物/前端/E2E；删除 018 迁移、`session_errors*.go` 与 `docs/impl/evidence/ADM-API-03h/`。
