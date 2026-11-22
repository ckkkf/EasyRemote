# ER-500 证据：WebTransport Spike/ADR

- 状态: ADR 接受（原生 raw QUIC；浏览器 WT 延后）
- 指标：未达 <2ms 本地目标，已记录退路

## 1. 产出

| 文件 | 内容 |
|---|---|
| docs/adr/ADR-0001-webtransport.md | 候选/方法/数据/决策/风险 |
| core/.../examples/transport_spike.rs | 可复现回环握手/吞吐/RTT 基准 |
| quic.rs | 增加只读 rtt() |

## 2. 原始数据

debug:
    {"handshake_ms":6.172,"sent":4000,"received":2443,"bytes":2706844,"mbps":35.8,"rtt_us":22174,"elapsed_ms":604.6}

release:
    {"handshake_ms":1.638,"sent":4000,"received":1662,"bytes":1841496,"mbps":28.0,"rtt_us":3059,"elapsed_ms":525.5}

## 3. 结论

- 原生：raw quinn datagram/stream，保持现有实现。
- 浏览器：wtransport 适配层延后到 ER-503；本次未构建对照，开销 <3% 未验证。
- 延迟 3.059ms > 2ms，吞吐 28Mbps 不足 1080p60；回环突发丢包 58%，证明发送节奏/背压必需（ER-601/602）。
- 退路：原生 raw QUIC + Relay 兜底；Web 控制继续 HTTP/WS。

## 4. 复现

    cd core
    cargo run -p er-transport --example transport_spike
    cargo run --release -p er-transport --example transport_spike

## 5. 边界

- 回环不等于真机/WAN；需 ER-903 故障注入与真实网络。
- wtransport 对照/二进制体积/证书配置未测。
- 未改生产传输路径，避免 Spike 影响现网。

## 6. 回滚

- ADR 可被后续基准推翻；届时新增 ADR 修订，不删除本记录。
