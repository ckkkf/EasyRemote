# ER-200 证据：会话 command + version + CAS

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 变更

| 文件 | 目的 |
|---|---|
| store/store.go | Session.Version；UpdateSessionCAS；ErrVersionConflict |
| store/memory.go | 创建 version=1；CAS 版本比较；旧 Update 递增 |
| store/pg.go | version 列读写；CAS WHERE version=$expected |
| store/migrations/001_init.sql（两份） | version 列 + ALTER IF NOT EXISTS |
| session/session.go | Command/Apply；迁移白名单；CAS 重试；幂等 no-op |
| session/session_test.go | 旧“重复操作必须失败”断言更新为幂等 |
| session/state_machine_test.go | 5 条新测试 |

## 2. 迁移语义

| 当前 | 命令 | 结果 |
|---|---|---|
| requested | consent accept | authorized + auth window；version+1 |
| requested | consent reject | closed；CONN_CONSENT_REJECTED |
| requested | consent（超时） | closed(consent_timeout)；CONN_CONSENT_TIMEOUT |
| authorized | activate | active；version+1 |
| authorized/active | activate 重复 | no-op；version 不变 |
| any | close | closed；version+1 |
| closed | close/consent reject | no-op；version 不变 |
| requested | activate | 拒绝；state/version 不变 |
| closed | consent accept | SESSION_NOT_FOUND |

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| requested→active 非法且不改版本 | TestIllegalTransitionRequestedToActive | PASS |
| 重复 accept 幂等 | TestDuplicateConsentIdempotent | PASS |
| ExpectedVersion 冲突 | TestCommandExpectedVersionConflict | PASS |
| 并发 accept/kill 收敛 closed 且 version>=2 | TestConcurrentAcceptAndKillCAS（30 次） | PASS |
| 超时主动关闭 | TestConsentTimeoutClosesViaCommand | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/session/ ./internal/gateway/ ./internal/adminapi/ ./internal/adminhttp/ ./internal/store/ ./internal/stack/
    ok（全部）
    $ bash scripts/gate.sh   # 含 go test -race
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- Request 仍用 ListSessions 扫描查重（未做唯一索引/CAS 创建）；同一对设备并发首次请求可能各建一条。归 ER-204/ER-801。
- admin 列表/API 暂未暴露 version；ER-810/812 可加入。
- 事件/outbox/审计尚未与迁移同事务，归 ER-201/ER-203。

## 6. 回滚

- 回滚 CAS 会恢复丢更新，禁止。
- 回滚 version 列需保留 DEFAULT 1 兼容旧数据；不得删除。
