# 第二轮：错误契约 · 服务加固 · 信令 · 管理后台

> 日期：2026-11-20 ｜ 承接 [review-round1.md](./review-round1.md)

## 一、本轮完成的项

| # | 项 | 说明 |
|---|---|---|
| 1 | **错误信封契约**（S2/S3） | 新增 `internal/httpx`：`{code,name,message,retryable,retry_after_ms,trace_id}`；message 取自码表，detail 只进日志；未结构化错误统一转 `SRV_INTERNAL` |
| 2 | **HTTP 加固**（S4） | 新增 `internal/serve`：ReadHeaderTimeout 5s / ReadTimeout 15s / WriteTimeout 30s / IdleTimeout 60s + 优雅关闭；`httpx.Decode` 带 1MB 上限且拒绝未知字段；`httpx.Middleware` 提供 panic 恢复与 trace |
| 3 | **信令抽包 + 会话绑定**（S11/S12） | 新增 `internal/signal`：Hub 管理在线表与会话绑定；候选/授权只在绑定两端转发；同设备重连关旧连接；`Unregister` 只在连接归属时删除；读 deadline 每帧续期；单帧 64KB 上限 |
| 4 | **React 管理后台** | `web-admin`：Vite + React 18 + TS(strict)，登录（密码 + TOTP）、仪表盘、用户、设备、会话、审计六页；错误信封解析成 `ApiError`（含 trace_id） |
| 5 | **前端契约测试** | `/tmp/contract.mjs` 复现前端调用序列，12 项断言全通过 |

## 二、验证证据

### 服务端

| 项 | 结果 |
|---|---|
| `gen-errors.mjs --check` | ✔ 87 条三端一致 |
| `gofmt -l` / `go vet` | 无输出 |
| `go test ./...` | **12 个包全绿** |
| `go test -race ./...` | 12 个包全绿 |
| `go test -tags=integration` | 通过 |

### 信令端到端（真实 WebSocket，8 项断言）

| 断言 | 结果 |
|---|---|
| HELLO → HELLO_ACK | ✅ |
| 查询在线状态 | ✅ |
| 请求连接 → 被控端收到 CONSENT_REQUIRED | ✅ |
| **攻击者替被控端授权被拒** | ✅ |
| 被控端授权 → 主控收到 CONSENT_GRANTED | ✅ |
| **攻击者注入 ICE 候选被丢弃** | ✅ |
| 合法候选送达对端 | ✅ |
| 未绑定会话不转发 | ✅ |

### 前端契约（12 项断言）

| 断言 | 结果 |
|---|---|
| admin 登录 200 且返回不透明令牌 | ✅ |
| 绑设备返回 device_code / device_token | ✅ |
| overview / users / devices / sessions / audit 五个接口 200 且字段名匹配 | ✅ |
| 用户行含 `phone_mask`，**不含 `phone_enc`** | ✅ |
| 审计记录 `admin.login` | ✅ |
| 错误信封含 code/name/message/retryable/trace_id | ✅ |

### 构建

| 项 | 结果 |
|---|---|
| `cargo test`（er-common） | 13 通过 |
| `npm run build`（web-admin） | 成功，151.85 kB / gzip 48.70 kB |
| `tsc -b` strict | 无错误 |

## 三、仍未完成（objective 相关）

| # | 缺口 | 影响 |
|---|---|---|
| 1 | Rust 核心只有 `er-common`（协议 + 错误码），`er-capture`/`er-codec`/`er-transport` 仍是空目录 | 客户端无法采集/编码/传输 |
| 2 | Flutter 客户端 UI 未开始（环境无 Flutter SDK） | 客户端不可用 |
| 3 | Store 仍无 context / error 返回（S10）、验证码竞态未修（S8）、session id 可枚举（S9） | PG 实现前必须改 |
| 4 | 后台口令仍是裸 SHA256、TOTP 无重放防护 | 安全项 |
| 5 | 管理后台未做登录后的可视化验证（仅契约测试 + 登录页截图） | 体验未验证 |

## 四、下一轮建议

1. Store 重构（context + error + CAS）+ 随机 ID → 修掉 S8/S9/S10。
2. Rust `er-transport` trait + 内存实现 + 契约测试（与 Go 侧协议对拍）。
3. 后台口令 argon2id + TOTP 重放防护。
4. 管理后台补登录态可视化验证（Playwright 注入 token 后截图）。
