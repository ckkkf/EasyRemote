# ER-204 证据：Request 复用与并发唯一

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 行为

| 旧会话状态 | Request 行为 |
|---|---|
| requested 未超时 | 复用 |
| requested 超时 | close(request_expired)，新建 |
| authorized 窗口有效 | 复用 |
| authorized 窗口过期 | close(auth_window_exceeded)，新建 |
| active | 复用 |
| closed | 忽略，新建 |

## 2. 并发唯一

- Memory：锁内扫描同 pair 的 state != closed，存在则返回已有。
- PG：事务内 pg_advisory_xact_lock(controller|target) → SELECT live → INSERT。
- 迁移：sessions_live_pair_idx（partial unique index WHERE state <> 'closed'）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 关闭会话不复用 | TestRequestDoesNotReuseClosedSession | PASS |
| 超时 requested 收口后新建 | TestRequestDoesNotReuseExpiredRequested | PASS |
| 授权过期 authorized 收口后新建 | TestRequestDoesNotReuseExpiredAuthorized | PASS |
| 16 并发首次请求只建 1 条 | TestConcurrentFirstRequestCreatesOne | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/session/ -run 'TestRequestDoesNotReuse|TestConcurrentFirstRequest'
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 创建 requested 会话尚未写 outbox 事件（ER-203 已记录），归下一张事件一致性卡。
- PG 唯一索引对历史重复数据可能迁移失败；当前基线无重复，生产需先清理（ER-410）。
- Request 仍扫描最多 500 条历史会话；分页/索引优化归 ER-801。

## 6. 回滚

- 回滚过期收口会复用超时会话，禁止。
- 回滚并发唯一会产生双会话，禁止。
