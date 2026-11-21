# 第二十三轮：客户端信令接入（部分完成，缺口已定位）

> 日期：2026-11-20 ｜ 目标轮次 14/20

## 一、本轮做了什么

| 层 | 内容 |
|---|---|
| Rust | `er-client::signalling`：WebSocket 客户端（tungstenite），HELLO 认证 + 等待 CONSENT_REQUIRED |
| Rust | `TargetAgent::wait_for_session(signal_base, timeout)`：被控端自己从信令发现会话 |
| Go | `stack.BuildRendezvous`：把信令处理抽成可装配的 handler，**与网关共用同一 store** |
| Go | `MountRendezvous`：一体化二进制同时提供 HTTP API 与 /ws |

## 二、已验证的部分

### 服务端信令（2 条新测试）

```
TestRendezvousAcceptsRegisteredDevice   ✅  刚绑定的设备能通过 HELLO 认证
TestRendezvousRejectsUnauthenticated    ✅  无凭证返回 DISCONNECT(12)
```

第一条是这轮的关键：之前 `cmd/rendezvous` 与 gateways 是两个进程、两份内存 store，
刚绑定的设备在信令侧永远「未授权」。抽成 `BuildRendezvous(st, secret)` 后共享同一 store，问题消失。

### 客户端信令

Rust 客户端能连上 ws、完成 HELLO 认证（`Signalling::connect` 返回 Ok 才会进入等待阶段）。
失败点出现在等待 `CONSENT_REQUIRED`：`Connection reset without closing handshake`。

## 三、缺口在哪（已定位，不是猜测）

```
主控 → HTTP POST /v1/connect/request  → 网关创建 session
                                        ↓
                                   ❌ 这里没有通知信令 Hub
                                        ↓
被控端 ← WebSocket 等待 CONSENT_REQUIRED ← 永远等不到
```

**网关的 `/v1/connect/request` 只写数据库，没有把事件推给信令层。**
两个进程部署时这需要一条跨进程通道（Redis Pub/Sub 已有基础设施）；
一体化部署下可以直接注入 hub 的 `NotifyConsentRequired`。

另外 `serve.Run` 的 `ReadTimeout: 15s` 会杀掉长连接 WebSocket，
需要为 /ws 路径单独设置（或把 Server 的 ReadTimeout 置 0 并对普通请求用 Middleware 限时）。

## 四、验证汇总

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | 18 passed |
| 信令服务端 | 2 条新测试通过 |
| 信令端到端 | **未通过**（网关未通知 Hub） |

**本轮没有把信令端到端跑通，不标记为完成。**

## 五、下一步

1. 网关 `/v1/connect/request` 成功后调用 `hub.NotifyConsentRequired`（一体化注入；分部署走 Redis）
2. `/ws` 路径豁免 ReadTimeout
3. 补一条真正的端到端：主控发起 → 被控从 WS 收到 → 授权 → 传帧
