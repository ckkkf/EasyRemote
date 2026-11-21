# 第九轮：装 Flutter · QUIC 真实传输 · 审查 B1–B6 修复 · 认证联调

> 日期：2026-11-20

## 一、Flutter 已装好

`brew install --cask flutter` → **Flutter 3.50.0 / Dart 3.14.0**（macOS arm64）。
客户端 UI 不再受环境限制。

## 二、QUIC 真实传输（不再是假实现）

新增 `core/crates/er-transport/src/quic.rs`：

| 能力 | 实现 |
|---|---|
| 连接 | quinn 0.11 + rustls 0.23（自签证书 + 证书 pin） |
| 视频/音频 | QUIC DATAGRAM，超 MTU 直接丢帧不排队 |
| 控制/数据 | 单向 Stream，有序队列 + 后台写任务 |
| 监听 | `QuicListener::bind` 先拿地址与证书，再 `accept` |

### 修掉的三个真问题（都是测试逼出来的）

1. **死锁**：旧 `accept_one` 要等连接建立才返回地址，而客户端要先拿到地址才能连 → 永远等不到。拆成 bind + accept。
2. **Endpoint 生命周期**：quinn 的连接依赖 endpoint 存活，函数返回时 endpoint 析构 → 连接被关。改为在结构体里持有。
3. **可靠帧丢失**：可靠帧入队后异步发送，发送方立即返回并析构连接 → 排队中的 pong 永远发不出去。新增 `flush(timeout)`，关闭前必须排空。

端到端测试：真实 QUIC 连接，双向往返 datagram 与 reliable stream，**通过**。

## 三、审查阻断项修复

第二轮独立审查（reviewer2）给出 6 个阻断 + 11 个严重。本轮修了：

| 编号 | 问题 | 修复 |
|---|---|---|
| B3 | relay 生产环境回退开发者密钥 | `loadSecret` 改走 `config.Secret()`，生产缺配即退出 |
| B4 | 生产装配写入 root/dev-pass 超管 | `SeedDevAdmin` 生产环境直接返回错误 |
| B5 | 生产 gateway 用固定验证码 123456 | `BuildGateway` 强制注入 `SMSProvider`；`devSMS()` 生产返回 nil 触发装配失败 |
| B6 | 信令 HELLO 不校验身份，可冒充任意设备 | HELLO 要求 `auth_token` + `device_token`，且校验设备归属该账号 |
| A14 | `ER_ENV=prod` 拼写错误静默退化成开发模式 | 环境名归一化，未知值一律按生产处理（fail-safe） |

另外修了 **并发迁移死锁**：两实例同时启动执行 DDL 触发 `40P01 deadlock detected`，改用 `pg_advisory_lock` 串行化。

## 四、联调验证（真实 PG + Redis + 双信令节点）

```
无凭证 HELLO 被拒            ✅ 返回 DISCONNECT {reason:"unauthorized"}
认证后上线成功               ✅
跨实例在线查询 online=true    ✅
跨实例 CONSENT_REQUIRED      ✅
跨实例 CONSENT_GRANTED       ✅
跨实例 ICE 候选              ✅
```

## 五、全量验证

| 项 | 结果 |
|---|---|
| Go 单测 | 16 包全绿 |
| Go 竞态 | 16 包全绿 |
| vet / gofmt | 无输出 |
| 集成（内存） | 通过 |
| PG 契约 | 通过 |
| Redis 契约（4 个包） | 通过 |
| 生产守卫测试 | 通过 |
| **Rust** | **37 passed**（含 QUIC 端到端） |

## 六、尚未修复（审查剩余）

严重项 S1–S11 仍未处理，其中三条会影响联调表现：

- **S1** 限流 Lua 只在新 key 设 TTL：历史无 TTL 的 key 会永久封禁某主体
- **S2** TOTP 重放 key 是全局 step 而非 (admin, step)：同 30 秒内只有第一个管理员能登录
- **S5** Unregister 异步清在线表：可能误删新连接的 presence

阻断项 B1（QUIC 背压/静默丢帧）只做了 flush 与失败计数，**无界队列仍在**。

## 七、下一步

1. 修 S1/S2/S5（都是几行改动，但影响可用性）
2. 修 B1：mpsc 改有界 + 失败标志 + 可观测
3. Flutter 客户端骨架：六页（登录/设备列表/会话中/诊断/设置/本机）
4. 补 D1 网络诊断、D2 防睡眠、D3 水印
