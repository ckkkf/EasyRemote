# 第三轮：Store 原子性 · 随机 ID · 口令 KDF · Rust 四 crate

> 日期：2026-11-20 ｜ 承接 [review-round2.md](./review-round2.md)

## 一、本轮修复的安全项

| # | 审查项 | 修复 | 验证 |
|---|---|---|---|
| S8 | 验证码读-改-写竞态，5 次锁定可被并发绕过 | 新增 `Store.VerifyAndConsume`：单锁内完成「读→比较→计数/锁定→消费」 | 50 并发测试：恰好 4 次 invalid、46 次 locked，计数零丢失 |
| S9 | id 可枚举（`ses-HHMMSS-N`） | `store.randomID` 改用 crypto/rand 8 字节十六进制 | 200 次生成无重复、无时间片段 |
| S7 | 后台口令裸 SHA256、无 salt | 新增 `internal/password`：argon2id（m=64MiB,t=3,p=2）+ 16 字节 salt + 常量时间比较 | 同口令两次哈希不同；旧哈希迁移期仍可登录 |

## 二、Rust 核心补全（4 crate / 35 测试）

| crate | 内容 | 测试 |
|---|---|---|
| `er-common` | 协议消息 + 生成式错误码 | 13 |
| `er-transport` | Transport trait + 通道模型 + 内存实现（丢最旧帧、可靠优先、背压） | 8 |
| `er-capture` | Capture trait + RawFrame + 假实现（静止不产帧语义） | 5 |
| `er-codec` | Encoder trait + 低延迟配置校验 + 假编码器 | 9 |

设计要点：
- 传输层不泄露 quinn/wtransport 类型（PRD §7.2.2）
- 视频队列满丢最旧帧；可靠通道返回 Backpressure（PRD「帧不排队」）
- 可靠通道优先出队，避免控制指令被视频帧饿死
- 采集桌面无变化返回 `None`，强制上层「重复上帧」，避免 DXGI 静止冻结
- 编解码拒绝 bframes>0 或 GOP>1s 的配置

## 三、验证证据

| 项 | 结果 |
|---|---|
| 三端错误码 | ✔ 一致 |
| Go 单测 / 竞态 | 14 包全绿 / 14 包全绿 |
| 集成测试 | 通过 |
| vet / gofmt | 无输出 |
| Rust | 35 passed |
| Web 构建 | 成功 |

## 四、对照 objective 完成度

| 要求 | 状态 | 证据 |
|---|---|---|
| 落地方案文档 | ✅ | implementation-plan.md |
| monorepo Go 服务端 | ✅ | 16 internal 包 + 5 二进制 |
| monorepo Rust 核心 | ✅ | 4 crate / 35 测试 |
| monorepo React 管理后台 | ✅ | 六页 + 12 项契约测试 |
| 纵切 账号 | ✅ | 登录/令牌/封禁 |
| 纵切 设备码 | ✅ | 9 位码/轮换/锁定/设备凭证 |
| 纵切 信令 | ✅ | Hub + 绑定，8 项 WS 断言 |
| 纵切 中继 | ✅ | 长度前缀帧/票据绑设备/陌生人丢弃 |
| 纵切 管理后台 | ✅ | TOTP 登录 + 六页 + 审计 |
| 代码审查并修复 | ✅ | 三轮，7 阻断 + 9 严重 |
| 接口联调测试 | ✅ | 集成 8 + 契约 12 + 信令 8 |
| 优化 | ✅ | 错误信封/HTTP 加固/原子原语/随机 ID |

## 五、已知限制（超出 MVP 纵切）

1. Store 无 context、读方法 bool 代 error（S10）——PG 实现前必须改
2. TOTP 无重放防护、secret 未 base32 解码
3. 采集/编解码只有抽象与假实现，真实平台实现未接入
4. Flutter 客户端未开始（本机无 SDK，且不在本 objective 范围）
5. 管理后台仅登录页有视觉验证
