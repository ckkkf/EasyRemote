# 第十四轮：数据面闭环打通（采集→编码→中继→解码）

> 日期：2026-11-20 ｜ 目标轮次 5/20

## 一、本轮目标

上一轮结束时，服务端能转发、客户端能编解码，但两者从未真正通过一条链路说过话。
这一轮把「客户端 ↔ 真实 Go 中继」接上。

## 二、新增 `er-session` crate

| 模块 | 内容 |
|---|---|
| `ticket` | 中继票据签发/校验，**逐字节对齐 Go `relaymgr`** |
| `wire` | 帧格式、注册体、分片/重组 |
| `relay_client` | UDP 客户端：注册、发送、接收、超时 |

### 票据格式（两边必须完全一致）

```
base64url_nopad(sessionID|self|peer|exp) . base64url_nopad(HMAC-SHA256(payload))
```

字段顺序错一个位置，表现就是「能连上端口但永远注册失败」——所以专门写了跨语言测试钉住。

### 分片

视频帧 76 KB，超过单个 UDP 包承载能力，应用层切分：

```
[msgID:u32][idx:u16][total:u16][payload]
```

重组器支持乱序到达与重复包。

## 三、真机端到端实测

```
source 1920x1080, encoded 76423 bytes
sent in 10 fragment(s)
received 76423 bytes via relay
decoded 1920x1080, 6220800 rgb bytes
RELAY E2E PASSED
```

链路：**采集真实屏幕 → H.264 编码 → 切 10 片 → 真实 Go 中继进程 → 重组 → 解码回 1080p RGB**。

## 四、排查过程（值得记录）

第一次跑，中继日志里**一个字都没有**——连「验签失败」都没有。用 Python 手工发了一个包，中继立刻报 `verify failed`，说明网络路径没问题，是 Rust 侧压根没发出去或发错地址。

真正的原因在接收侧：**Go 中继转发时只发 `body`（已剥掉帧头），而 Rust 客户端把收到的数据当成完整帧去解析**，于是分片头被误读成帧头，永远解析不出数据。

修复：接收侧直接把整包当作裸分片。

这个 bug 的特征很典型——发送完全正常、中继日志干净、接收方一动不动。如果没有 Python 那个对照实验，很容易误判成「中继丢了包」。

## 五、跨语言兼容测试（Go 侧，4 条）

把 Rust 实际产出的票据与注册帧硬编码进 Go 测试：

```
TestRustTicketAcceptedByService      ✅
TestRustRegisterFrameParses          ✅
TestRustTicketPayloadLayout          ✅  payload = ses-e2e-1|devA|devB|2000000000
TestForwarderRoutesRegisteredPair    ✅
```

这条测试的价值：以后任何一方改协议，另一方立刻红。

## 六、顺带修的

- 中继加了 `verify failed` / `plan failed` 日志。之前这类失败是静默 `continue`，排查时完全看不到。
- 空载荷分片（`total=0`）会导致接收方永远等不到——修正为显式 `total=1`。

## 七、验证

| 端 | 结果 |
|---|---|
| Rust | **102 passed**（89 → 102） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| vet | 无输出 |
| Flutter | 13 passed |
| 真实中继端到端 | 通过（1080p 全链路） |

## 八、还没做的

1. **ICE 打洞**：现在只能走中继，直连没实现（PRD §7.2.3 的另一半）
2. **端到端加密**：中继转发的是明文（PRD §9「中继只见密文」目前只体现在架构上）
3. 输入事件还没走这条链路回传
4. 断线重连、弱网降级矩阵
5. 硬件编解码与 40ms 达标
