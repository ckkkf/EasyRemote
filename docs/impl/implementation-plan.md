# EasyRemote 代码落地方案

> 依据：[PRD v0.8](../prd/easyremote-prd-v0.8.md) · [协议草案](../spec/protocol.md) · [错误码表](../spec/error-codes.md) · [页面清单](../spec/pages.md) · [原型](../../prototype/index.html)
> 版本 v1.0 ｜ 2026-11-20 ｜ 状态：执行中

---

## 一、三条硬约束

写第一行代码前先摆约束，因为它们决定模块怎么切。

| 约束 | 来源 | 对代码结构的影响 |
|---|---|---|
| C1 协议要能同时被 Rust 客户端与浏览器（V2 Web 主控）实现 | 协议草案 §1 | 传输层必须「协议定义」与「传输实现」分离，quinn/wtransport 类型不许漏进业务层 |
| C2 管理后台与客户端 API 物理隔离 | PRD §6.1 | 独立二进制、独立端口、独立鉴权中间件、独立数据库角色 |
| C3 中继只转发密文 | PRD §9.1 | Relay 必须是 L4 转发，代码里不允许出现解析媒体流的逻辑 |

---

## 二、仓库结构

```
EasyRemote/
├── server/                 Go：客户端 API、信令、中继、后台 API
│   ├── cmd/{gateway,rendezvous,relay,admin}/
│   ├── internal/
│   │   ├── errcode/       错误码常量（三端一致的第一环）
│   │   ├── store/         存储接口 + 内存实现 + PG 实现
│   │   ├── auth/          账号、令牌、短信验证码
│   │   ├── device/        设备码、临时验证码、信任关系
│   │   ├── session/       会话生命周期、授权窗口
│   │   ├── relaymgr/      中继票据与配额
│   │   ├── adminapi/      后台业务 + 审计
│   │   └── config/
│   ├── migrations/
│   └── go.mod
├── core/                   Rust：采集、编解码、传输
│   └── crates/{er-common,er-capture,er-codec,er-transport}/
├── web-admin/              React + TS
├── design/                 设计令牌
├── docs/                   需求与规格
└── prototype/              原型
```

---

## 三、依赖方向

```
        errcode  ← 零依赖，三端共享语义
           │
      ┌────┴────┐
      ▼         ▼
    store    session
      │         │
      └────┬────┘
           ▼
   auth  device  relaymgr  adminapi
           │
           ▼
        cmd/*   只做装配
```

**硬规则**
1. `cmd/*` 不许出现 SQL 与业务判断，只装配依赖。
2. `errcode` 不许 import 任何业务包。
3. `relaymgr` 只认 `SessionTicket`，不认媒体类型。
4. 任何包不许自建数据库连接，一律走 `store` 注入。

---

## 四、分期与代码顺序

### 第 1 期：服务端纵切（本轮）

| 顺序 | 模块 | 验收 |
|---|---|---|
| 1 | `errcode` | 与 spec 一致 + 单测 |
| 2 | `store` | 接口 + 内存实现，同一套用例 |
| 3 | `auth` | 注册/登录/令牌/频控 单测 |
| 4 | `device` | 设备码唯一、验证码过期、5 次锁定 单测 |
| 5 | `session` | 状态迁移、12h 授权窗口、幂等 单测 |
| 6 | `relaymgr` | 票据签发/校验/过期/配额 单测 |
| 7 | `adminapi` | 每个写操作必产审计 |
| 8 | `cmd/*` | 四个二进制可启动，/healthz 200 |

### 第 2 期：客户端核心

`er-common`（协议+错误码，纯逻辑）→ `er-transport`（trait + 内存实现 + WebTransport）→ `er-capture`/`er-codec`（trait + Windows 实现）。

### 第 3 期：Flutter UI + 后台前端

按 `docs/spec/pages.md` 编号推进，先 C2/C3/C4/C6/C7 主干。

---

## 五、接口契约

### 5.1 客户端 API（gateway :8080）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/auth/sms` | 发送验证码 |
| POST | `/v1/auth/login` | 手机号 + 验证码登录 |
| GET | `/v1/devices` | 设备列表 |
| POST | `/v1/devices/bind` | 绑定设备，返回设备码 |
| POST | `/v1/connect/request` | 发起连接 |
| POST | `/v1/connect/consent` | 被控端授权/拒绝 |
| GET | `/v1/connect/status` | 建连状态 |

### 5.2 信令（rendezvous /ws）

按协议草案 §4.1，type 号不变。首期实现 HELLO / QUERY_DEVICE / REQUEST_CONNECT / CONSENT_* / CANDIDATES / DISCONNECT。

### 5.3 中继（relay UDP）

`session` 签发 `SessionTicket`（session_id、双方 device_id、过期、HMAC）→ relay 校验 → 双向转发 → 统计字节。**不解析载荷**。

### 5.4 管理后台 API（admin :8081）

| 方法 | 路径 | 权限 |
|---|---|---|
| POST | `/admin/v1/login` | 公开（密码 + TOTP） |
| GET | `/admin/v1/stats/overview` | 只读+ |
| GET | `/admin/v1/users` | 只读+ |
| POST | `/admin/v1/users/{id}/ban` | 风控 |
| GET | `/admin/v1/devices` | 只读+ |
| GET | `/admin/v1/sessions` | 只读+ |
| POST | `/admin/v1/sessions/{id}/kill` | 客服（必填原因） |
| GET | `/admin/v1/audit` | 只读+ |

---

## 六、数据模型

```sql
accounts(id, phone_hash, phone_enc, status, created_at)
devices(id, device_code, account_id, platform, pubkey, last_seen_at)
verify_codes(device_code, code_hash, expires_at, attempts)
trust_edges(controller_device_id, target_device_id, expires_at)
sessions(id, controller_id, target_id, state, started_at, ended_at, quality_json)
admin_users(id, username, password_hash, totp_secret, role, status)
audit_logs(id, actor, role, action, object, before, after, ip, result, ts)
configs(key, value, version, updated_by)
```

约束：device_code 唯一索引；audit_logs 仅 INSERT/SELECT；手机号加密存储、查询走 phone_hash。

---

## 七、错误码三端一致

`server/internal/errcode` 与 `core/crates/er-common` 必须逐条一致。
校验脚本 `scripts/check-errors.mjs`：从 `docs/spec/error-codes.md` 解析 code/name，比对两端常量，不一致则退出码非 0。

---

## 八、测试策略

| 层次 | 手段 | 目标 |
|---|---|---|
| 单元 | Go testing + testify | 每个 internal 包核心分支 |
| 契约 | 表驱动，跑同一组用例于内存实现 | store 接口 |
| 集成 | 起真实进程 + HTTP 串完整流程 | 注册→绑设备→连接→授权→会话→后台中断 |
| 安全 | 专项：爆破锁定、越权、中断权限 | 对应错误码必须被触发 |

---

## 九、审查清单

1. 依赖方向是否被破坏（cmd 里有没有业务逻辑）
2. 错误是否都返回结构化错误码，不是裸 fmt.Errorf
3. 写操作是否产审计
4. 敏感字段是否脱敏后才出接口
5. 并发路径的锁粒度
6. 时间比较是否用 UTC
7. 是否有 secrets 硬编码
8. 测试是否覆盖失败路径

---

## 十、本轮执行范围

- ✅ 本文档
- ✅ errcode + 一致性校验脚本
- ✅ store 接口 + 内存实现
- ✅ auth / device / session / relaymgr + 单测
- ✅ adminapi + 审计
- ✅ 四个 cmd 可启动
- ✅ 集成测试跑通完整流程
- ⬜ Rust 核心（下一轮）
- ⬜ Flutter UI（下一轮）

---

## 十一、风险与对策

| 风险 | 对策 |
|---|---|
| 无 PostgreSQL/Redis 实例 | store 走接口 + 内存实现；PG 实现按同接口补 |
| 短信网关未接入 | SMSProvider 接口，测试用固定码 |
| 工具链差异 | Rust 已装 stable（含 clippy），Flutter 缺失，客户端 UI 延后 |

---

## 十二、执行进度（滚动更新）

| 期次 | 内容 | 状态 |
|---|---|---|
| 第 1 期 | 服务端纵切（账号/设备/会话/中继票据/后台 API） | ✅ 完成 |
| — | 代码审查 + 7 个阻断项修复 | ✅ 完成（见 review-round1.md） |
| — | 错误信封 contract + HTTP 加固 | ✅ 完成 |
| — | 信令抽包 + 会话绑定 + 端到端 | ✅ 完成 |
| — | React 管理后台（六页 + 契约测试） | ✅ 完成（见 review-round2.md） |
| 第 2 期 | Rust 客户端核心（er-transport/capture/codec） | ⬜ 未开始 |
| 第 3 期 | Flutter 客户端 UI | ⬜ 未开始（环境无 Flutter SDK） |
| — | Store 重构（context/error/CAS）+ 随机 ID | ⬜ 未开始 |
