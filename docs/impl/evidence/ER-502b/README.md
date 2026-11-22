# ER-502b 证据：P2P 链路切换（pair）

- 状态: 完成（pair 会话 P2P + link；生产独立进程未接线）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-session/relay_client.rs | 候选交换、并行 punch、ready、activate、send/recv 切换、link_kind |
| er-client/pair.rs | 注册后 P2P 升级；PairReport.link |
| er-ffi/session.rs | JSON 增加 link |
| client/session_e2e_test.dart | 断言 link=p2p（回环） |

## 2. 语义

- 双方各自创建 p2p socket，通过 Relay 交换 host 候选（ERLINK1）。
- 双方并行 punch；各自发送 ERLINK2 ready。
- 只有双方 ready=true 才切 P2P；否则清 p2p_peer 保持 Relay。
- P2P 发送失败 → 立即回退 Relay；接收优先 P2P poll，再 Relay。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 候选编解码/token 校验 | candidate_exchange_roundtrip_and_token_check | PASS |
| 单方成功不切换 | activate_p2p_requires_both_sides | PASS |
| 会话实际走 P2P 且上报 | session_e2e（link=p2p） | PASS |
| 既有 E2E | host_link/session_e2e | PASS |

## 4. 命令与输出

    $ cd core && cargo test -p er-session relay_client
    3 passed
    $ cd client && flutter test --concurrency=1 test/session_e2e_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 仅 pair/同进程会话完成；生产 host_stream/ctrl_session 仍只走 Relay，需 ER-502c 接线。
- 回环 P2P 成功；NAT/对称 NAT/失败切换 E2E 未做（ER-903）。
- token 由 session_id 派生（弱）；接入生产必须用会话密钥/认证上下文。
- 接收 P2P 1ms poll + Relay 500ms，UI 性能需重新评估。

## 6. 回滚

- 回滚切换会恢复全 Relay，功能可用但 P2P 能力丢失。
- 不可回滚成“单方切 P2P”，会造成半连接。
