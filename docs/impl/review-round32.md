# 第三十二轮：弱网矩阵（用户态丢包代理）+ er-ffi 接硬件编码

> 日期：2026-11-20 ｜ 机器：Mac(macOS, 主控/服务) + WIN-VERIFY-PC(Windows 11, 被控)
> 证据目录：`docs/impl/evidence/weaknet-2026-11-20/`
> 约束：不用 tc netem（需要 sudo），改用仓库内用户态 UDP 代理注入；全程未启用代理/VPN。

---

## 一、这一轮做了什么

### 1.1 er-ffi 接上硬件编码（App 侧能吃到 NVENC）

- `core/crates/er-ffi/src/lib.rs`：会话持有的编码器从 `OpenH264Codec` 换成
  `er_codec::create_encoder`（Windows 上 NVENC 可用即硬件，否则软件），类型改为 `Box<dyn Encoder>`。
- 新增两个 C ABI：
  - `er_hardware_encoder_available() -> int`：本机是否有硬件编码（**真开过一次 NVENC 会话**才算）
  - `er_encoder_kind(sess) -> char*`：本会话实际用的编码器（`nvenc`/`software`/空串）
- `client/lib/native.dart`：加对应绑定与 `ErNative.hardwareEncoderAvailable()`、
  `NativeSession.encoderKind()`。

验证：

```
$ cargo test -p er-ffi
test result: ok. 5 passed; 0 failed; 0 ignored

$ nm -gU target/debug/liber_ffi.dylib | grep er_hardware_encoder_available
00000000000060d8 T _er_hardware_encoder_available

$ python3 -c "...ctypes 调 er_hardware_encoder_available / er_encoder_kind(None)..."
hw_available= 0          # macOS 无 NVENC，符合预期
kind(null)= b''          # 无会话返回空串

$ flutter test
00:18 +18: All tests passed!
```

Windows 侧同一份代码（`create_encoder` 在 RTX 3060 上返回 `EncoderKind::Nvenc`）已在
第三十轮的跨机会话里被验证（日志 `CODEC: kind=Nvenc 2880x1620`）。
本轮**没有**在 Windows 上重新跑 er-ffi 的 cdylib（被控机当前锁屏，且 agent-3 正在占用构建机做 DXGI 基准），
所以「Windows 上 er_encoder_kind 返回 nvenc」这条仍是**间接证据**，未直接测。

### 1.2 弱网矩阵

丢包代理 `server/cmd/lossyproxy`（Go，约 170 行）：

- `-listen / -target / -loss / -delay / -jitter / -reorder / -seed`
- 每个下游源地址一条独立上游 socket（中继按源地址识别端位，必须保持）
- 双向各自按 `loss` 丢包；固定 seed 可复现；每 2s 打印 `down_in/down_drop/up_in/up_drop`

冒烟自测（0.5 丢包，双向，100 次请求-应答）：

```
sent=100 replied=27        # 期望 100 * 0.5 * 0.5 = 25，实测 27
stats down_in=100 down_drop=45 up_in=55 up_drop=28
```

拓扑（丢包只作用在被测端那一段，不影响 Mac 与中继之间）：

```
weaknet_send(Mac) → stack/relay(Mac, 真实服务) → lossyproxy(:19100, 丢包) → udptun → ssh -R → Windows weaknet_recv
```

探测协议（探针自带，不是生产协议）：`[1][idx][ts][offset][h264]`，另有 PING/PONG 用来估
收发两端时钟偏移，从而把「单向时延」算准（不假设两台机器时钟同步）。

---

## 二、结果（原始输出见证据目录）

| 组 | 分辨率 | 每帧分片 | 注入丢包 | 发送 | 送达 | 整帧丢失 | 应用层丢帧率 | 解码成功 | 解码失败 |
|---|---|---|---|---|---|---|---|---|---|
| A | 320x240 | 1 | 0% | 300 | 300 | 0 | **0.0%** | 300 | 0 |
| B | 320x240 | 1 | 5% | 300 | 284 | 16 | **5.3%** | 195 | 89（占送达 31.3%） |
| C | 320x240 | 1 | 15% | 300 | 260 | 40 | **13.3%** | 95 | 165（占送达 63.5%） |
| D | 1280x720 | 2 | 5% | 150 | 135 | 15 | **10.0%** | 69 | 66（占送达 48.9%） |

时延（单向，已用 PING/PONG 校正时钟偏移）：

| 组 | p50 | p95 | max | 接收跨度 |
|---|---|---|---|---|
| A | 28ms | 31ms | 76ms | 11024ms |
| B | 29ms | 39ms | 68ms | 11152ms |
| C | 27ms | 30ms | 56ms | 11141ms |
| D | 30ms | 46ms | 70ms | 24993ms |

代理侧计数（证明丢包真的注入了）：

- B：`up_in=303 up_drop=16` → 数据报丢 5.3%
- C：`up_in=303 up_drop=40` → 数据报丢 13.2%
- D：`up_in=315 up_drop=15` → 数据报丢 4.8%，但整帧丢 10.0%

### 2.1 读出来的三件事

1. **单分片帧：帧丢失 ≈ 数据报丢失。**
   320x240 每帧只有约 1.9KB，一个数据报装得下，所以 5% → 5.3%，15% → 13.3%，
   基本 1:1 传导，没有放大。

2. **多分片帧：帧丢失被放大成 1-(1-p)^n。**
   1280x720 每帧约 12.2KB，中继按 8KB 分片 → 每帧 2 个数据报。
   数据报丢 4.8% 时，整帧丢失理论值 `1-(1-0.048)^2 = 9.4%`，实测 **10.0%**，吻合。
   这解释了为什么 1080p（每帧 6~9 个分片）在同等丢包下会明显更糟——
   **要么做 FEC/重传，要么把分片做大（当前 8KB 上限受 MTU 约束），要么让关键帧周期性重发。**

3. **真正的杀手不是丢帧本身，而是参考链断裂。**
   5% 丢包时只有 16/300 帧彻底没到，但 89/284 帧解不出来（31%）；
   15% 时 165/260 帧解不出（63%）。因为 P 帧依赖前一帧，前面一丢，
   后面全废，直到下一个 IDR（本组 GOP=60）。
   → 结论：**必须在丢包路径上主动请求关键帧**（`Encoder::request_keyframe`），
   现在的数据面没有做这件事；也没有把接收侧丢包反馈给发送侧。

### 2.2 ABR 到底有没有被触发

**没有。** 这一条要写清楚：

- `er-abr` 里是一套纯逻辑控制器（`AbrController::on_feedback`），输入是
  `LinkFeedback { loss_pct, rtt_ms, queue_ms }`。
- 但数据面（`er-client` 的 `RelayClient`/`TargetAgent`/`ControllerAgent`）**没有把接收侧的丢包统计
  回传给发送侧**，也没有任何地方周期性调用它。也就是说，ABR 是个"离线可用的函数"，
  在真实会话里**一次都没跑过**。
- 把本轮实测的链路反馈喂进去会怎么决策（`examples/abr_curve.rs`，纯离线仿真，不是触发证据）：

```
loss%    rtt(ms)  level    bitrate  changed/reason
0.0      70       1080p60   8000    false / 链路健康，保持
5.0      70        900p30   3500    true  / 轻微拥塞，降一档
15.0     70        540p30   1500    true  / 丢包/积压严重，连降两档
```

要把 ABR 变成"真的被触发"，至少还缺三块：
接收侧按周期统计 `lost/total` 与 RTT → 通过反向通道回传 →
发送侧按周期调用 `on_feedback` 并把 `QualityLevel` 应用到 `EncodeConfig`（重配编码器）。
这三块本轮都没做，只能如实标注为未实现。

---

## 三、顺手发现的一个真实缺陷（网关）

做第 4 组（720p）时连续失败，服务端日志是：

```
trace=... code=700005 name=ADMIN_FORBIDDEN detail=session party credential required
```

可疑代码在 `server/internal/gateway/handler.go` 的 `/v1/connect/request`：

```go
ctrlDevices, listErr := a.Device.List(ctx, acc.ID)
...
controllerID := ctrlDevices[0].ID     // ← 取账号下"第一个"设备
```

会话的 `controller_id` 取的是**账号下列表第一个设备**，而不是本次请求里
`x-device-token` 对应的那个设备；而 `/v1/relay/ticket` 用
`requireDevice(sess.ControllerID)` 校验，所以两者一旦不是同一台设备就 700005。

**已确认的事实（有原始日志）**：
- 同一手机号（13700006xxx 系列）反复绑定新设备、多次建会话之后，取票据连续 12 次
  返回 `700005 session party credential required`（`/tmp/erbin/stack.log`，见证据目录 stack 片段）；
- 换成全新手机号后同一套代码立刻跑通（组 D 就是这么跑出来的），说明不是代码/网络问题。

**未确认的部分**：`Device.List` 的排序规则我没验证（新→旧 还是 旧→新），
所以"第 N 次绑定时 `devices[0]` 恰好不是本次设备"这一机制属于**推断**，不是结论。
要下定论需要看 store 层 SQL 的 ORDER BY，或者直接打印 `sess.ControllerID` 与请求头设备做对比。

本轮处理：**没有改网关代码**（不在我这轮分工里），只记录现象 + 可疑点；
建议下一步：`controller_id` 直接用请求携带的 `x-device-token` 解析出的设备，
并对"账号下多设备"补一条集成测试。

---

## 四、指标改善 / 仍不达标

| 项 | 状态 | 说明 |
|---|---|---|
| 丢包注入可复现 | ✅ | 用户态代理，seed 固定，双向计数可见，无需 sudo |
| 丢包 ↔ 帧丢失传导 | ✅ 已量化 | 单分片 ≈1:1；双分片 4.8% → 10.0%（理论 9.4%） |
| 时延受丢包影响 | 🟡 基本无影响 | p50 稳定 27~30ms；本组是恒定速率发送，没有排队，所以丢包不改变时延，只减少帧 |
| 参考链断裂 | ❌ 未缓解 | 5% 丢包即 31% 帧解不出、15% 时 63% 解不出；缺"丢包触发关键帧请求" |
| ABR 触发 | ❌ 未实现 | 纯逻辑可算，但数据面没有反馈回路，会话中从未被驱动 |
| FEC / 重传 | ❌ 未做 | 多分片放大问题未缓解 |
| 端到端 40ms 预算 | ❌ 未达标 | 本轮只测传输层；编码在 Mac 上是软件路径（720p 129.8ms/帧），Windows 侧 NVENC 已 7.8ms |
| er-ffi 硬件编码 | 🟡 部分 | 代码已接 `create_encoder`，macOS 上跑通（hw=0 合理）；Windows 上未直接验证 cdylib 导出 |

---

## 五、复现（照抄）

```bash
# 0) 依赖
go build -o /tmp/erbin/lossyproxy ./server/cmd/lossyproxy
go build -o /tmp/erbin/{stack,relay,udptun} ./server/cmd/{stack,relay,udptun}
GOOS=windows GOARCH=amd64 go build -o /tmp/erbin/udptun.exe ./server/cmd/udptun

# 1) Mac 服务：stack(18090) + relay(19000) + lossyproxy(19100→19000, 丢包 p) + udptun server(19001→19100)
/tmp/erbin/stack  -addr 127.0.0.1:18090 &
/tmp/erbin/relay  -addr 127.0.0.1:19000 &
/tmp/erbin/lossyproxy -listen 127.0.0.1:19100 -target 127.0.0.1:19000 -loss 0.05 -seed 22 &
/tmp/erbin/udptun -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19100 &

# 2) 把两个入口反向映射到 Windows
ssh -N -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &

# 3) Windows：UDP 隧道客户端 + 接收器（Session 0 即可，不做采集/注入）
udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
weaknet_recv.exe http://127.0.0.1:18090 127.0.0.1:19000 <phone_recv> 123456 http://127.0.0.1:18090 C:\EasyRemote\win_hello.txt 12

# 4) Mac：读 hello 再发帧
scp win:C:/EasyRemote/win_hello.txt /tmp/win_hello.txt
./target/debug/examples/weaknet_send http://127.0.0.1:18090 127.0.0.1:19000 <phone_send> 123456 /tmp/win_hello.txt 300 16 loss5
# 多分片对照：把最后两个参数换成 1280 720，帧数给 150
```

> 注意：被测的 receiver 手机号必须是**全新账号**（见第三节的网关缺陷），
> 否则 `/v1/relay/ticket` 会返回 700005。
