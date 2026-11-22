# ER-501 证据：可靠注册与分片

- 状态: 完成（ACK + 分片边界；gate PASS）
- 限制：真实弱网注入未做。

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-session/wire.rs | FRAME_REGISTER_ACK、ERACK1 魔数、MAX_FRAGMENTS=4096 |
| er-session/relay_client.rs | register 等 ACK/重试；pending 缓存；1200B 分片；ACK 过滤 |
| relay/forwarder.go | FrameRegisterAck；MaxDatagram=2048 |
| cmd/relay/main.go | 注册成功后回 ACK（ERACK1+dev） |

## 2. 语义

- 注册：发送 → 等 ACK（500ms）→ 最多 3 次；无 ACK 返回 NotRegistered。
- 注册期间对端先到的数据进入 pending，recv 先消费，不丢。
- 迟到重复 ACK 用 ERACK1 前缀过滤。
- 分片：载荷 ≤1200B；单消息分片数 ≤4096；中继读缓冲 2048。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 收到 ACK 注册成功 | register_waits_for_ack | PASS |
| 无 ACK 注册失败 | register_without_ack_is_not_registered | PASS |
| 超限分片被拒 | oversized_fragment_count_rejected | PASS |
| E2E 会话/主机 | session_e2e / host_link | PASS |

## 4. 命令与输出

    $ cd core && cargo test -p er-session
    38 passed
    $ cd server && go test ./internal/relay/
    ok
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 未做真实丢包/抖动/ACK 丢失率；ER-903。
- 注册 ACK 只由接收注册的节点发送；跨节点转发注册仍走目录，未单独测。
- 1200B 分片会增加分片数（大帧更多包），需要媒体分片节奏（ER-601/602）。
- E2E 首次失败是 FFI dylib 未重建（仍用旧 8192 分片），重编后通过；gate 顺序已保证 rust 先于 flutter。

## 6. 回滚

- 回滚 ACK 会恢复“发送成功=已连接”的假连接，禁止。
- 回滚 1200B 会重新引入 IP 分片放大，禁止。
