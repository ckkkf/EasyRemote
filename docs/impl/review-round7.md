# 第七轮：把「同类问题」全部改成共享状态

> 日期：2026-11-20 ｜ 承接 [review-round6.md](./review-round6.md)

## 一、什么算「同类问题」

判据：**这个状态在多实例部署时会不会失效或被绕过**。按这个标准扫全仓，找到三处：

| 状态 | 位置 | 多实例后果 |
|---|---|---|
| 后台会话 | `adminsession` | A 实例签发、B 实例 401（已在上一轮修） |
| **限流计数** | 原本完全没有 | 打散请求即可绕过验证码爆破、短信轰炸、登录爆破 |
| **TOTP 重放防护** | 原本完全没有 | 同一 6 位码在不同实例重复使用 |
| **信令在线表 / 会话绑定** | `internal/signal` 进程内 map | 两台设备连到不同实例时，候选与授权无法互相传递 |

## 二、逐项修复

### 1. 限流（新增 `internal/ratelimit`）

接口 `Limiter`，两套实现：内存（单实例）、Redis（跨实例）。

Redis 实现用 Lua 保证 `INCR + EXPIRE` 原子：

```lua
local current = redis.call('INCR', KEYS[1])
if current == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
return {current, redis.call('PTTL', KEYS[1])}
```

接入点：

| 位置 | 限制 |
|---|---|
| `/v1/auth/sms` | 同手机号 1 次/分钟；同 IP 10 次/小时 |
| `/v1/auth/login` | 同手机号 10 次/小时 → `AUTH_TOO_MANY_ATTEMPTS` |
| `/v1/connect/request` | 同账号 30 次/分钟 → `CONN_RATE_LIMITED` |
| `/admin/v1/login` | 同用户名 5 次/10 分钟 |

### 2. TOTP 加固

| 项 | 修复 |
|---|---|
| 与标准认证器不互通 | secret 改为 **base32 解码**（兼容历史明文 secret） |
| 无重放防护 | 新增 `ReplayGuard`：内存实现（单实例）/ Redis `SETNX`（跨实例，fail-closed） |
| 时间步不可测 | `verifyTOTP` 返回命中的时间步，便于测试与审计 |

### 3. 信令跨实例（`internal/signal/bus.go`）

新增 `Bus` 接口，Redis 实现三件事：

| 能力 | 实现 |
|---|---|
| 消息投递 | Pub/Sub 频道 `er:signal:dev:<deviceID>` |
| 会话绑定共享 | Hash `er:signal:bind:<sessionID>`，10 分钟 TTL |
| 在线表共享 | `er:signal:online:<deviceID>`，5 分钟 TTL，**只允许本实例清除**（Lua 比对实例名） |

`Hub` 的转发逻辑改为：本地有连接直接发；没有就走总线。绑定查询本地 miss 时回退共享存储。

## 三、验证

### 契约测试（同一套用例跑两种实现）

| 模块 | 用例 | 内存 | Redis |
|---|---|---|---|
| adminsession | issue_verify_revoke / revoke_all / missing_token | ✅ | ✅ |
| ratelimit | limit_enforced / reset / namespaced_keys | ✅ | ✅ |

### 双实例跨 Redis 信令（真实两个进程 + 真实 Redis）

```
两台设备分别连到不同实例                     ✅
跨实例在线查询返回 online=true               ✅
跨实例投递 CONSENT_REQUIRED                  ✅
跨实例回传 CONSENT_GRANTED                   ✅
跨实例转发 ICE 候选                          ✅
```

这条是本次最关键的验证：**测试过程中确实先失败过**——绑定只存在于主控所在实例，被控端在另一个实例上读不到，授权结果发不回来。加上共享绑定后才通过。

### 全量

| 项 | 结果 |
|---|---|
| 错误码三端一致 | ✔ |
| gofmt / vet | 无输出 |
| Go 单测 | **16 包全绿** |
| Go 竞态 | 16 包全绿 |
| 集成（内存） | 通过 |
| PG 契约（真实 PostgreSQL 15） | 通过 |
| Redis 契约（会话/限流/信令） | 全通过 |
| Rust | 35 passed |
| Web 构建 | 成功 |

## 四、生产部署必配

| 变量 | 缺失后果 |
|---|---|
| `DATABASE_URL` | 拒绝启动 |
| `REDIS_URL` | 拒绝启动（会话、限流、重放防护、信令总线全部依赖它） |

## 五、剩余项

1. 中继（relay）的 `Forwarder` 端位表仍是进程内——UDP 天然绑定单个节点，但如果要多节点中继，需要按 session 做节点路由。
2. 采集/编解码只有 trait 与假实现。
3. Flutter 客户端未开始。
