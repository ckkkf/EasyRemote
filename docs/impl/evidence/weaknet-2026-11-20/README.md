# 弱网矩阵证据（2026-11-20）

被测链路：Mac(主控, weaknet_send) → 真实 gateway/rendezvous(stack) + 真实 UDP 中继(relay)
→ **用户态丢包代理(lossyproxy)** → udptun(UDP over TCP) → ssh -R → Windows(被控, weaknet_recv)

丢包注入点：中继 → 被控端 方向（视频方向），双向各按比例丢；不涉及 sudo/tc，不动系统网络。

| 文件 | 说明 |
|---|---|
| recv-loss0.log / send-loss0.log | 0% 基线（320x240，300 帧） |
| recv-loss5.log / send-loss5.log | 5%（320x240，300 帧） |
| recv-loss15.log / send-loss15.log | 15%（320x240，300 帧） |
| recv-loss5-720p.log / send-loss5-720p.log | 5%（1280x720，150 帧，每帧 2 分片） |
| lossyproxy-loss*.log | 代理自身的 down/up 计数（丢了多少个数据报） |
| abr-curve.txt | ABR 纯逻辑离线曲线（**不是**会话里被触发的证据） |

recv-*.log 是从 Windows 机器 scp 回来的原始输出；send-*.log 是 Mac 终端原始 stdout。

工具：`server/cmd/lossyproxy`（Go，参数化丢包/时延/抖动/乱序，固定 seed 可复现）、
`core/crates/er-client/examples/weaknet_send.rs`、`.../weaknet_recv.rs`。
