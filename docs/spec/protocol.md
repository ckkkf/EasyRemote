# EasyRemote 协议草案

> 配套文档：[PRD v0.7](../prd/easyremote-prd-v0.7.md) ｜ [错误码表](./error-codes.md)
> 版本 **v0.1 草案** ｜ 2026-11-19 ｜ 状态：需求已冻结，本文为 Spike 与 MVP 的实现依据

---

## 1. 传输基线

**WebTransport over HTTP/3**（见 PRD §7.2.1）

| 通道 | 载体 | 可靠性 | 用途 |
|---|---|---|---|
| `media` | WebTransport datagram（HTTP/3 Datagram, RFC 9297） | 不可靠、可丢、无序 | 视频帧、音频帧 |
| `control` | unidirectional stream（短生命周期） | 可靠、有序 | 输入事件、心跳、统计 |
| `data` | bidirectional stream | 可靠、有序、可流控 | 文件、剪贴板 |

**为什么视频走 datagram**：可靠传输意味着丢包要重传，重传对实时视频是灾难（等到重传完这帧已经过期）。宁可丢帧也不要延迟。

## 2. 编码格式

| 用途 | 格式 | 说明 |
|---|---|---|
| 控制消息 | **Protobuf**（`.proto`） | 有 schema、可演进、跨语言 |
| 媒体帧 | **自定义二进制头 + 编码数据** | 每帧都要解析，protobuf 开销不可接受 |
| 文件块 | 自定义头 + 裸数据 | 同上 |

### 2.1 控制消息信封

```
+--------+--------+--------+--------+
| magic  | ver    | type   | flags  |   4 bytes
+--------+--------+--------+--------+
| payload_len (u32 BE)               |   4 bytes
+------------------------------------+
| protobuf payload                   |   N bytes
+------------------------------------+
```

- `magic` = `0x45 0x52`（"ER"）
- `ver` = `0x01`
- `type` = 消息类型（见 §4）
- `flags` = bit0: 需要响应；bit1: 是响应；其余保留

### 2.2 媒体帧头

```
+-----------+------------+------------+-----------+-------------+
| frame_type| stream_id  | seq (u16)  | flags     | ts_ms (u32) |
|   (u8)    |   (u8)     |            |   (u8)    |             |
+-----------+------------+------------+-----------+-------------+
| 可选扩展头（按 flags）                              |
+-----------------------------------------------------+
| 编码数据（H.264/H.265/AV1 NALU 或 PCM/Opus）          |
+-----------------------------------------------------+
```

| 字段 | 说明 |
|---|---|
| `frame_type` | 1=video, 2=audio, 3=cursor, 4=stats |
| `stream_id` | 多显示器 / 多轨道时区分（0 = 主屏） |
| `seq` | 16 位循环序号，用于检测丢包与乱序 |
| `flags` | bit0: keyframe；bit1: 有扩展头；bit2: 帧结束 |
| `ts_ms` | 采集时间戳（毫秒，32 位循环） |

**尺寸约束**：单 datagram ≤ 1200 bytes（保守值，避开 MTU 分片）。大于此值必须由编码层分片（`flags` bit2 标识帧结束），或走 `data` 通道（关键帧可选）。

## 3. 时序：一次完整建连

```
主控端                 Rendezvous                  被控端
  │                        │                         │
  │── HELLO ──────────────▶│                         │
  │◀─ HELLO_ACK ───────────│                         │
  │                        │                         │
  │── QUERY_DEVICE ───────▶│                         │
  │◀─ DEVICE_STATUS ───────│ (online? nat_type?)     │
  │                        │                         │
  │── REQUEST_CONNECT ────▶│── CONNECT_INCOMING ────▶│
  │                        │                         │
  │                        │◀─ CONSENT_PENDING ──────│ (用户看到弹窗)
  │◀─ CONSENT_REQUIRED ────│                         │
  │                        │                         │
  │── SUBMIT_CAPTCHA ─────▶│                         │
  │                        │◀─ CONSENT_RESULT(ok) ───│ (用户点接受)
  │◀─ CONSENT_GRANTED ─────│                         │
  │                        │                         │
  │◀──────────── CANDIDATES 交换（双向）─────────────▶│
  │                        │                         │
  │══════════ 打洞尝试(并行, 3s 超时) ═══════════════│
  │   成功 → P2P 直连                                   │
  │   失败 → 走 Relay                                   │
  │                        │                         │
  │◀════════ WebTransport 会话建立 ═══════════════════▶│
  │◀════════ KEY_EXCHANGE (X25519 + HKDF) ════════════▶│
  │◀════════ CAPS 协商 ═══════════════════════════════▶│
  │◀════════ START_STREAM ════════════════════════════▶│
  │◀════════ 首帧 ════════════════════════════════════▶│
```

**关键约束**
- 授权窗口：`CONSENT_GRANTED` 之后 12 小时内重连不再弹授权（PRD §5.1.3）。
- 打洞与中继**并行准备**，打洞失败立即切中继，不等超时后再建。
- 密钥协商必须在任何媒体帧之前完成。

## 4. 消息清单

### 4.1 信令消息（客户端 ↔ Rendezvous）

| type | 名称 | 方向 | 字段 |
|---|---|---|---|
| 0x01 | `HELLO` | C→S | `client_ver, platform, device_id, auth_token, pubkey` |
| 0x02 | `HELLO_ACK` | S→C | `server_time, heartbeat_interval_ms, min_client_ver` |
| 0x03 | `QUERY_DEVICE` | C→S | `device_code` |
| 0x04 | `DEVICE_STATUS` | S→C | `online, nat_type, last_seen_ms` |
| 0x05 | `REQUEST_CONNECT` | C→S | `device_code, role, client_caps` |
| 0x06 | `CONNECT_INCOMING` | S→C | `controller_nick, controller_acct_tail, ip_region, role` |
| 0x07 | `CONSENT_REQUIRED` | S→C | `request_id, need_captcha` |
| 0x08 | `SUBMIT_CAPTCHA` | C→S | `request_id, code` |
| 0x09 | `CONSENT_RESULT` | S↔S | `request_id, accepted, reason_code` |
| 0x0A | `CONSENT_GRANTED` | S→C | `request_id, session_id, auth_expire_ms` |
| 0x0B | `CANDIDATES` | C↔C | `ice_ufrag, candidates[]`（经 S 转发） |
| 0x0C | `DISCONNECT` | C→S | `session_id, reason_code` |

### 4.2 对端消息（主控 ↔ 被控，经 P2P 或 Relay）

| type | 名称 | 方向 | 字段 |
|---|---|---|---|
| 0x20 | `PEER_HELLO` | 双向 | `session_id, role, nonce` |
| 0x21 | `KEY_EXCHANGE` | 双向 | `x25519_pub, salt` |
| 0x22 | `CAPS` | 双向 | `codecs[], max_res, max_fps, features[]` |
| 0x23 | `START_STREAM` | 双向 | `streams[{ch, codec, w, h, fps}]` |
| 0x24 | `INPUT_EVENT` | 主控→被控 | 见 §5 |
| 0x25 | `CLIPBOARD_UPDATE` | 双向 | `kind, size, hash`（数据走 data 通道） |
| 0x26 | `PRIVACY_SCREEN` | 被控→主控 | `enabled` |
| 0x27 | `DISPLAY_LIST` | 被控→主控 | `displays[{id, w, h, scale, primary}]` |
| 0x28 | `QUALITY_REPORT` | 双向 | `fps, rtt_ms, loss_pct, bitrate_kbps, encode_ms, decode_ms` |
| 0x29 | `CONTROL_REQ` | 双向 | `action(take_over|resize|refresh_keyframe)` |
| 0x2A | `BYE` | 双向 | `reason_code` |

### 4.3 心跳与保活

| 机制 | 参数 |
|---|---|
| 应用层 PING | 空闲 1s 发一次 |
| 判定断线 | 连续 3 次无 PONG（约 3s） |
| 重连退避 | 1 / 2 / 4 / 8s，最多 10 次，总计 ≤ 60s |
| 授权有效期 | 12h（重连不重新授权） |

## 5. 输入事件编码

`INPUT_EVENT` 的 payload：

```protobuf
message InputEvent {
  enum Kind {
    KEY_DOWN = 0; KEY_UP = 1;
    MOUSE_MOVE = 2; MOUSE_BUTTON = 3; MOUSE_WHEEL = 4;
    TOUCH = 5; GAMEPAD = 6;
  }
  Kind kind = 1;
  uint64 ts_ms = 2;

  // 键盘：用扫描码而非字符，避免输入法冲突
  uint32 scancode = 3;
  uint32 modifiers = 4;   // bitmask: ctrl/alt/shift/win

  // 鼠标：绝对坐标（0..65535 归一化）或相对位移
  uint32 x = 5;
  uint32 y = 6;
  sint32 dx = 7;
  sint32 dy = 8;
  uint32 button = 9;      // 1=left 2=right 3=middle
  bool   pressed = 10;

  // 触控
  repeated TouchPoint touches = 11;

  // 显示器
  uint32 display_id = 12;
}
```

**设计要点**
1. **键盘用扫描码**，不用 Unicode 字符——否则输入法、快捷键、游戏按键全乱。
2. **鼠标绝对坐标归一化到 0..65535**，与分辨率解耦，跨屏换算简单。
3. `ts_ms` 用于被控端做输入平滑与延迟统计。
4. 游戏场景切相对位移模式（`dx/dy`），避免指针漂移。

## 6. 文件传输

```
data 通道（bidirectional stream）
  ├─ FILE_OPEN   { path, size, mode }
  ├─ FILE_CHUNK  { offset, len, data[] }        每块 1–4 MB
  ├─ FILE_ACK    { offset, bitmap[] }           接收方确认
  └─ FILE_CLOSE  { ok, hash }
```

| 项 | 规定 |
|---|---|
| 块大小 | 1 MB（默认），可协商至 4 MB |
| 校验 | 块级 xxHash64；全文件再校验一次 |
| 断点续传 | 接收端持久化已收块位图，重连后发 `FILE_ACK` 带位图 |
| 并发 | 单会话最多 4 个文件并行 |
| 限速 | 默认不超过链路估计带宽的 50%，可配置 |

## 7. 版本演进规则

1. **消息只增不改**：新增字段给默认值，不修改已有字段语义。
2. **type 号不复用**：废弃的 type 保留占位，标注 deprecated。
3. **能力协商**：`CAPS` 消息里声明 features 列表，双方取交集；不支持的特性静默不启用，不报错。
4. **最低版本**：服务端在 `HELLO_ACK` 里下发 `min_client_ver`，低于则返回 `100010`。

## 8. 安全

| 项 | 规定 |
|---|---|
| 信令 | TLS 1.3 |
| 媒体 | 端到端加密，X25519 ECDH + HKDF-SHA256 派生，ChaCha20-Poly1305 加密 |
| 密钥位置 | 只在两端内存，中继不持有 |
| 重放防护 | nonce + 时间窗（±30s） |
| 证书 | 客户端校验服务端证书，**不提供忽略选项**（对应错误码 200016） |

## 9. 未决问题

1. 关键帧丢失后如何请求重传？当前设计用 `CONTROL_REQ.refresh_keyframe`，但需要定义触发阈值。
2. 多显示器时是一个 stream 内多 stream_id，还是多条独立 stream？当前草案用 stream_id 区分（省连接数），需 Spike 验证切换延迟。
3. 音频与视频的唇音同步策略未定（V1 才做音频）。
4. Relay 转发时是否需要在应用层加自己的序号（用于统计），还是纯粹 L4 转发？
