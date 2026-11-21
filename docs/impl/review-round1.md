# 代码审查与修复记录（Round 1）

> 审查对象：`server/**` ｜ 方式：独立审查代理，只读 + PoC 验证
> 修复日期：2026-11-20 ｜ 结论：**阻断项 7/7 已处理**

## 一、阻断项

| # | 问题 | 状态 | 修复方式 |
|---|---|---|---|
| B1 | 后台身份可伪造（base64 解码即身份） | 已修 | 新增 `internal/adminsession`：登录签发 32 字节随机令牌，服务端存表，角色只取自会话表 |
| B2 | 登录令牌与校验令牌是两套格式，无过期 | 已修 | `Authenticate` 只验身份；令牌由 adminsession 统一签发/校验/撤销 |
| B3 | verify-code / consent / relay-ticket / status 无鉴权 | 已修 | 绑定设备时返回一次性 `device_token`；四接口强制 `X-Device-Token`；consent 只接受被控端；票据按本端签发 |
| B4 | 中继可被陌生人注入、第三方抢端位 | 已修 | 抽出 `internal/relay`：票据绑定 `Self`；未登记地址丢弃；目标只取已登记对端 |
| B5 | 固定 36 字节 session id，真实 id 转不动 | 已修 | 帧格式改 `[type][u16 sidLen][sid][body]`，任意长度可用 |
| B6 | 硬编码密钥 + 万能验证码 + 默认超管 | 已修 | `internal/config`：生产要求 `ER_SECRET`≥16 字节且拒绝 dev 默认值；relaymgr 空密钥拒绝签发 |
| B7 | Activate 跳过 consent 与 12h 窗口 | 已修 | 状态迁移改显式白名单；Activate 要求 authorized 且窗口非零未过期 |

## 二、严重项（部分修复）

| # | 问题 | 状态 |
|---|---|---|
| S1 | 错误码语义错配 | 关键路径已改（Activate/Kill/Overview/Authenticate），完整映射表待补 |
| S2 | 内部 detail 直出 HTTP | 待修 |
| S3 | 错误信封缺 trace_id/retryable | 待修 |
| S4 | 无超时/body 上限/panic 恢复/优雅关闭 | 待修 |
| S5 | 审计失败被吞、登录无审计 | 已修：成功路径 fail-closed + `admin.login` 审计 |
| S6 | 权限校验分散 | 已修：Overview 下沉到 service |
| S7 | 口令无 KDF、MFA 可跳过、状态不校验 | 部分修：强制 MFA + 校验 Status；argon2id 待换 |
| S8 | 验证码并发竞态 | 待修（需 store CAS） |
| S9 | session id 可枚举 | 待修 |
| S10 | Store 无 context、bool 代 error | 待修（PG 前必须改） |
| S11 | cmd 里写业务逻辑 | 中继已抽包；rendezvous 待抽 |
| S12 | 信令无会话绑定、5 分钟必断 | 待修 |
| S13 | 中继资源泄漏 | 部分修：去掉只写不读的 addrOf |

## 三、验证证据

| 项 | 命令 | 结果 |
|---|---|---|
| 三端错误码 | `node scripts/gen-errors.mjs --check` | ✔ 87 条一致 |
| 格式 / 静态检查 | `gofmt -l` / `go vet` | 无输出 |
| 单测 / 竞态 | `go test ./...` / `-race` | 9 包全绿 |
| 集成 | `go test -tags=integration` | 通过（含 8 个安全回归） |
| Rust | `cargo test` | 13 通过 |
| 伪造后台令牌 | curl | **401**（修复前 200） |
| 无凭证轮换验证码 | curl | **拒绝**（修复前返回明文码） |

## 四、已知限制（带入下一轮）

1. 验证码并发竞态未修（需 store 原子原语）
2. session id 仍可枚举
3. 错误信封缺 trace_id / retryable
4. 信令未做会话绑定、deadline 不续期
5. `cmd/rendezvous` 未抽包，覆盖率 0
6. 后台口令仍是裸 SHA256
7. TOTP 无重放防护、secret 未 base32 解码

## 五、下一轮顺序

错误契约 → Store 重构 + 验证码 CAS + 随机 ID → 信令抽包 → 口令 KDF → HTTP 加固
