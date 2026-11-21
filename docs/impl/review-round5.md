# 第五轮：后台会话迁移到 Redis

> 日期：2026-11-20 ｜ 承接 [review-round4.md](./review-round4.md)

## 一、解决的问题

上一轮结束时，后台会话（`adminsession`）仍是进程内 map。后果：**多实例部署时，A 实例签发的令牌在 B 实例上无效**——负载均衡一开就随机 401。

## 二、实现

### 存储抽象

```go
type Store interface {
    Put(ctx, *Session) error
    Get(ctx, token) (*Session, error)
    Revoke(ctx, token) error
    RevokeAllForAdmin(ctx, adminID) error
    Ping(ctx) error
}
```

两套实现：`MemoryStore`（单实例/测试）、`RedisStore`（多实例）。

### Redis key 设计

| key | 类型 | 用途 | TTL |
|---|---|---|---|
| `er:adminsess:tok:<token>` | hash | 会话字段（admin_id/username/role/created_at/expires_at/revoked） | 与会话有效期一致 |
| `er:adminsess:adm:<adminID>` | set | 该账号全部 token，用于批量撤销 | 同会话 |

设计要点：
- **TTL 由 Redis 兜底**，不依赖应用侧清理；
- 撤销只把 `revoked` 置 1 而**不立即删除**，保留到 TTL 到期，便于审计期内查询；
- `RevokeAllForAdmin` 走 set 索引，不需要全表扫描。

### 新增登出接口

`POST /admin/v1/logout` —— 撤销当前 Bearer 令牌。

## 三、契约测试

与 Store 一样的做法：**同一套用例跑两种实现**。

```
issue_verify_revoke · revoke_all · missing_token
```

`TestMemoryStoreContract` 与 `TestRedisStoreContract` 共用 `runContract`。

## 四、验证证据

| 项 | 结果 |
|---|---|
| 错误码三端一致 | ✔ |
| gofmt / vet | 无输出 |
| Go 单测 | 14 包全绿 |
| Go 竞态 | 14 包全绿 |
| 集成（内存） | 通过 |
| PG 契约（真实 PostgreSQL 15） | 通过 |
| **Redis 契约（真实 Redis 7）** | **3/3 通过** |
| Rust | 35 passed |
| Web 构建 | 成功 |

### 全栈冒烟（PG + Redis 同时启用）

```
healthz → 200
后台登录（PG 账号 + Redis 会话）→ 返回 token
用该 token 访问 /admin/v1/stats/overview → 200
Redis 内 key 核对：
  er:adminsess:adm:adm-990f984debc999e8
  er:adminsess:tok:27Nz_t_aJqpd1Jb5VJbOOKbHYssRVFTGuVMiEWnGRXQ
```

说明会话确实落在 Redis 而不是进程内存。

## 五、部署矩阵

| 环境 | DATABASE_URL | REDIS_URL | 行为 |
|---|---|---|---|
| 本地/测试 | 空 | 空 | 内存 store + 内存会话 |
| 单实例 | 有 | 空 | PG store + 内存会话 |
| **多实例** | 有 | 有 | PG store + Redis 会话 |

## 六、仍未完成

1. TOTP 重放防护、secret base32 解码。
2. 限流/风控计数仍在进程内（多实例会失效，同样应搬 Redis）。
3. 采集/编解码只有 trait 与假实现。
4. Flutter 客户端未开始。
