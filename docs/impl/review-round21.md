# 第二十一轮：连续帧丢失的根因与修复（5/5）

> 日期：2026-11-20 ｜ 目标轮次 12/20

## 一、根因（不是网络问题）

上一轮记录的现象是「发 5 帧收 2 帧」。本轮把中继侧计数和分片级日志都打出来，定位到真正原因：

```
[frag] len=8200 msg=2 idx=0..9 total=10   ← 帧 1，正常
[recv] got message 80108 bytes
[frag] len=36  msg=3 idx=0 total=1        ← 帧 2：只有 28 字节载荷
[recv] got message 0 bytes                ← 解出 0 字节
[frag] len=36  msg=4 idx=0 total=1        ← 帧 3：同样
[frag] len=8200 msg=5 ... total=2         ← 帧 4，正常
[frag] len=36  msg=6 idx=0 total=1        ← 帧 5：同样
```

**36 字节 = 8 字节分片头 + 28 字节密文 = ChaCha20-Poly1305 对空载荷的封装（12 nonce + 16 tag）。**

也就是说：编码器对部分帧返回了**空 bitstream**，调用方照样加密（得到 28 字节）并发出去，
接收端把它当成一条「0 字节的完整消息」交付，白白消耗了接收配额，真正的帧反而没被计进来。

## 二、为什么编码器会返回空

OpenH264 的 `EncoderConfig` 默认 **`enable_skip_frame = true`**：
在码控压力下它会主动**丢帧**，`encode()` 返回空 bitstream。
OpenH264 自己的日志也印证了这一点：

```
Warning: bEnableFrameSkip = 0, bitrate can't be controlled without enabling skip frame
```

屏幕共享场景不能丢帧（丢帧就是画面卡住），必须关掉。

## 三、修复

```rust
let enc_cfg = EncoderConfig::new()
    .enable_skip_frame(false)                       // 关掉丢帧
    .set_bitrate_bps(cfg.bitrate_kbps * 1000)       // 显式码率
    .max_frame_rate(cfg.fps as f32)
    .usage_type(UsageType::ScreenContentRealTime)   // 屏幕内容优化
    .rate_control_mode(RateControlMode::Bitrate);
```

外加一道防线：发送端遇到空 bitstream **不发**，而不是发一条空消息。

## 四、修复后实测

```
sent 5 frames, received 5 frames
frame 0: 1920x1080 85545 bytes
frame 1: 1920x1080 5757 bytes
frame 2: 1920x1080 6189 bytes
frame 3: 1920x1080 6079 bytes
frame 4: 1920x1080 6685 bytes
AGENT E2E PASSED (5 frames via independent single-end agents)
```

断言已改回**严格相等**（原来是宽容的「至少 2 帧」）。

## 五、顺带修正的两处

1. **重组器支持多消息并发**（8 条在途）：原来只有单槽，新消息到达会丢弃未完成的上一条。
2. **`MAX_DATAGRAM` 从 64KB 降到 32KB**：macOS 的 UDP 载荷上限是 65507，写 64KB 会踩
   `Message too long (os error 40)`。同时发送端改为**始终分片**，不再试图整帧塞一个包。

## 六、验证

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | 15 passed |
| **单端代理 5 帧连续传输** | **5/5 通过** |

## 七、下一步

1. 单端代理接到 Flutter（controller / target 分开）
2. 断线重连（PRD §5.1.3）
3. ICE 打洞（目前强制走中继）
4. 硬件编解码与 40ms 达标
