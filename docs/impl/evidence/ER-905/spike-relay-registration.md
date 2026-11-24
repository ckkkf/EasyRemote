# Spike：ER-905-PROTO-01 relay 注册确认（blocker 证据）

## 结论

`host_stream.rs` 与 `mac_host.rs` 中的 **1200ms / 2500ms 两处固定等待**都无法用现有
协议消除。它们不是“凑时间”，而是对 UDP 中继**无注册确认**这一协议缺口的补偿。
要做掉它们，必须给 relay 增加注册 ACK 消息（`docs/spec/protocol.md` 变更）。

## 两处等待各自解决什么

### 1200ms：等「主控的 begin() 完成中继注册」

```rust
if target.consent(&session_id).is_err() { continue; }
sleep(1200ms);                                   // ← 此处
let (mut relay, mut crypto, msg) = target.begin(&session_id)?;
```

- 被控端 consent 后立刻 `begin()`，也立刻 `announce()` 自己的握手；
- 而主控侧的 `begin()` 需要先向网关申请 relay ticket，再 bind 本地 UDP 端口并注册；
- **relay 对「向未注册端发包」是静默丢弃**（服务端 `relay` 的 `drop` 计数即为此），
  UDP 没有连接语义，不会回 ICMP/错误；
- 因此被控端若在主控注册完成前 announce，握手包必然丢失，
  对端等 15s 后报 `no peer handshake`——这正是用户实测的那个 403/超时链条里的一环。

没有 ACK 时，唯一可用的判据只有时间：1200ms 是「本机与局域网实测下主控 begin 的 p99」。

### 2500ms：等「对端把我们的 announce 收进 relay 的 peer 表」

```rust
let (mut relay, mut crypto, msg) = target.begin(&session_id)?;
sleep(2500ms);                                   // ← 此处
target.announce(&mut relay, &msg)?;
```

- `announce` 之后对端要**再**读一次 relay 才能拿到我们的握手公钥；
- relay 的 peer 表是「收到第一个包才建立」，且两端注册顺序不保证；
- 2500ms 覆盖 swap 场景（先 announce 的一方要等另一方 begin 完成）。

## 为什么不能用「重试」替代

重试能提高成功率，但**重试的间隔本身仍是时间**；
而且 `finish()` 内部等 15s 才判定失败，重试会让一次失败会话占用 15s×N。
真正去掉固定等待需要协议提供：

1. relay 注册确认（`REGISTER_OK`），或
2. 握手包的 ACK / 序列号重传，或
3. 用可靠有序流做握手（媒体才走 datagram）

——三者都要改 `protocol.md`。

## 本卡处理

- 不删、不改这两处等待（删了会直接把 ER-905 的握手失败率拉回去）；
- 在两处都加了注释说明原因；
- 记录 blocker `ER-905-PROTO-01`，等 spec 变更批准后改为事件驱动。

## 现状更正

此前文档写「两处 sleep 已集中在一处」是**错的**：
`host_stream.rs` 有 1200ms + 2500ms，`mac_host.rs` 同样有两处，共四处。
