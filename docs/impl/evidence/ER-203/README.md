# ER-203 证据：领域事件与 outbox

- 状态: 完成（事件落库 + 分发器；gate PASS）
- 限制：消费者进程未接线；Request 创建事件未写。

## 1. 变更

| 文件 | 目的 |
|---|---|
| trace/trace.go | context 携带 trace_id |
| httpx/httpx.go | Middleware 注入 trace 到 request context |
| store/store.go | SessionEvent 模型 + 4 个 outbox 方法 |
| store/memory.go | 锁内 CAS + 事件去重（sessionID|version） |
| store/pg.go | 事务内 UPDATE CAS + INSERT ON CONFLICT DO NOTHING |
| migrations | session_events 表 + pending 索引 |
| session/session.go | changed 迁移写事件（old/new/reason/trace/version） |
| session/dispatcher.go | 幂等消费 + 失败 attempts/last_error |

## 2. 语义

- 事件在状态迁移同事务写入；CAS 冲突重试时不会产生重复事件。
- 同一 (session_id, version) 唯一；重复插入 DO NOTHING。
- 分发器读取未投递事件：handler 成功 → delivered；失败 → attempts+1/last_error，下轮重试。
- handler 契约：按 event ID 幂等（Dispatcher 保证成功后不再投递，但下游仍应幂等）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 迁移产生完整事件（含 trace/version） | TestTransitionWritesOutboxEvent | PASS |
| 成功只投一次 | TestDispatcherDeliversOnce | PASS |
| 失败保留并重试成功后 delivered | TestDispatcherRetriesFailedEvent | PASS |
| 既有状态机/并发/幂等 | ER-200/ER-201 测试 | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/session/ -run 'Outbox|Dispatcher'
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 尚未有生产 outbox 消费循环（session.Dispatcher 未接入 cmd）；事件目前只落库。
- Request 创建 requested 会话不写事件；需要创建+CAS 事件一体的存储方法（ER-204）。
- 后台审计仍写 audit_logs，未与 outbox 合并；操作审计与状态事件语义不同，ER-810 再统一展示。
- PG 版事务路径未在本机集成（无 PG），ER-410/ER-903 验证。

## 6. 回滚

- 回滚 outbox 会丢失状态迁移审计链，禁止。
- 回滚唯一键会产生重复事件，禁止。
