# ADR-0001: WebTransport / QUIC 传输选型（ER-500 Spike）

- 状态: 接受（原生 raw QUIC；浏览器 WebTransport 适配延后）
- 日期: 2026-11-22
- 相关: docs/spec/protocol.md 通道语义、ER-500/501/503、A-09

## 背景

协议要求：媒体走可丢 datagram，控制/心跳走可靠有序流，文件走可靠流控；需要 P2P/ICE 与 Relay 兜底。当前生产实现是裸 UDP + 自定义分片（A-09），控制不可靠、无浏览器复用、分片不可靠。

候选：

1. **wtransport**：Rust WebTransport 服务端/客户端（内部 quinn），浏览器互操作直接；
2. **quinn + h3**：QUIC 传输 + 自建 HTTP/3/WebTransport 会话层，控制力强、代码多；
3. **raw quinn**：仅原生客户端，使用 QUIC datagram/stream；浏览器需要额外网关。

## Spike 方法

新增可复现示例 core/crates/er-transport/examples/transport_spike.rs：
- 回环 QUIC 握手计时；
- 发送 4000 个 1100B 视频 datagram，统计接收数、吞吐、RTT；
- 命令：cargo run -p er-transport --example transport_spike（debug/release 各一次）。

### 结果（本机 macOS，loopback）

| 构建 | 握手 | 发送/接收 | 吞吐 | RTT |
|---|---|---|---|---|
| debug | 6.172 ms | 4000 / 2443 | 35.8 Mbps | 22.174 ms |
| release | 1.638 ms | 4000 / 1662 | 28.0 Mbps | 3.059 ms |

观察：
- 回环握手 1.6ms 可接受；
- 4000 帧突发在回环即出现 datagram 丢弃（接收 42%–61%），说明背压/节奏必须由发送端控制，不能把 send 成功当成对端收到；
- release RTT 3.06ms 已超过 PRD 延迟增量 <2ms 的本地目标，且这是回环下限，不能据此宣称达标。

## 决策

1. **原生客户端继续使用 raw quinn**（现有 er-transport::quic）：datagram/stream 语义与协议一致，不引入 HTTP/3 会话层开销。
2. **浏览器路线单独做 WebTransport 适配**：优先评估 wtransport 作为浏览器/边缘网关适配层（ER-503），不替换原生传输。
3. **Relay 是兜底而不是主路径**：P2P（ICE）与 Relay 选择由 ER-501/502 实现；关闭传播/票据撤销已在 ER-202/205 提供。
4. 媒体发送必须实现节奏与背压（保最新帧、按 RTT/丢包降码率），不能一次性灌 4000 帧；归 ER-601/602。

## 指标结论

- 握手：release 1.638ms（本机回环）— 可接受，但未在真机/WAN 验证。
- 带宽：release 28.0Mbps（debug 35.8Mbps）— 低于 1080p60 目标所需，且为回环；不能作为生产能力。
- 延迟：release RTT 3.059ms > 2ms 目标 — 未达标，按计划记录退路：原生 raw QUIC + Relay 兜底，WebTransport 仅浏览器适配。
- 开销 <3% 需要 wtransport 对照基准；本次未构建 wtransport，未验证。

## 后果与风险

- 原生路径可复用现有 QUIC 实现与测试；浏览器路线延后，Web 端继续使用 HTTP/WS 控制 + 后续 WebTransport。
- 必须补：wtransport 对照基准、真机/WAN 测量、发送节奏/丢包测试（ER-903）。
- 回退：若 wtransport 无法满足浏览器需求，使用 Relay + WebSocket 控制作为降级路径；媒体仍走 raw QUIC/Relay。

## 复现

    cd core
    cargo run -p er-transport --example transport_spike            # debug
    cargo run --release -p er-transport --example transport_spike  # release

输出为单行 JSON：handshake_ms/sent/received/bytes/mbps/rtt_us/elapsed_ms。
