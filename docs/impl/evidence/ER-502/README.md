# ER-502 证据：P2P 候选/STUN/打洞（模块）

- 状态: 部分完成（模块+测试；未接入会话编排）
- gate: PASS

## 1. 产出

| 文件 | 内容 |
|---|---|
| er-transport/src/p2p.rs | host 候选、STUN Binding、XOR-MAPPED、probe/ack、select_link |
| lib.rs / Cargo.toml | 导出模块、getrandom |
| tests | STUN 映射、loopback 打洞、失败回落 Relay |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| STUN 返回映射地址 | stun_query_returns_mapped_address | PASS |
| 回环打洞成功（含 ACK） | punch_loopback_succeeds | PASS |
| 无响应在超时内回落 Relay | punch_failure_falls_back_to_relay | PASS |

## 3. 命令与输出

    $ cd core && cargo test -p er-transport p2p
    3 passed
    $ cargo clippy -p er-transport --all-targets -- -D warnings
    exit 0
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 未完成与风险

- **未接入 conductor/pair**：没有通过信令交换 TypeCandidates、没有在会话真正选择 P2P/Relay，也没有链路上报；因此 A-09 尚未闭环。
- 只有 host/STUN IPv4；无 ICE 角色、TURN、NAT 矩阵。
- token 由调用方提供；接入时必须来自会话密钥/认证上下文，不能明文信令。
- 打洞超时是串行候选，未实现并行 race；3s 总预算可能被多候选取代。

## 5. 下一步

ER-502a：在 pair/conductor 中交换候选（signal TypeCandidates）、并行 race、选 Link 并上报 link_type；失败自动 Relay；补真实切换测试。
