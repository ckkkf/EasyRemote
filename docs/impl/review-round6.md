# 第六轮：把「开发默认内存」这个错误纠正掉

> 日期：2026-11-20

## 一、问题定性

前面几轮我先把存储写成内存实现、再"迁移"到 PG/Redis，顺序是错的。对一个要上线的服务，**持久层从一开始就该是 PG，内存只作测试替身**。这不是风格问题，它导致：

- 四个独立二进制（gateway/admin/rendezvous/relay）默认走内存，重启即丢数据；
- 生产环境即使忘配 `DATABASE_URL` 也能"正常启动"，故障延后到第一次写库才暴露；
- 后台会话在多实例下随机失效。

## 二、本轮的纠正

### 1. 环境驱动的存储装配

新增 `internal/stack/bootstrap.go`：

| 函数 | 规则 |
|---|---|
| `OpenStore(ctx)` | 有 `DATABASE_URL` → PG（自动迁移）；生产环境缺配置 → **返回错误，进程退出**；仅开发环境退回内存 |
| `OpenSessions(ctx, ttl)` | 有 `REDIS_URL` → Redis；生产环境缺配置 → **返回错误**；仅开发环境退回内存 |

### 2. 四个二进制统一走这套装配

`cmd/gateway` 与 `cmd/admin` 从"自己 new 内存 store"改为：

```go
st, err := stack.OpenStore(ctx)
sessions, err := stack.OpenSessions(ctx, 8*time.Hour)
app, err := stack.BuildGateway(st, secret)   // 或 BuildAdmin
```

### 3. 生产环境 fail-fast

```
$ ER_ENV=production ./er-gw
gateway: open store: stack: DATABASE_URL is required in production

$ DATABASE_URL=... ER_ENV=production ./er-admin
admin: open sessions: stack: REDIS_URL is required in production
```

不允许"生产环境悄悄跑内存"。

## 三、验证

### 单元/契约

| 项 | 结果 |
|---|---|
| Go 单测 | **15 包全绿** |
| Go 竞态 | 15 包全绿 |
| 集成（内存） | 通过 |
| PG 契约（真实 PostgreSQL 15） | 通过 |
| Redis 契约（真实 Redis 7） | 通过 |
| Rust | 35 passed |
| Web 构建 | 成功 |

新增 5 条环境守卫测试：

```
TestOpenStoreDevFallsBackToMemory
TestOpenStoreProductionRequiresDatabaseURL
TestOpenSessionsDevFallsBackToMemory
TestOpenSessionsProductionRequiresRedis
TestOpenStoreProductionWithDSNUsesPG
```

### 独立二进制冒烟（不是 stack）

```
gateway + PostgreSQL：
  healthz → 200
  登录 + 绑设备 → 返回 device_code / device_token
  PG accounts 表确有新行

admin + PostgreSQL + Redis：
  登录 → 返回 token
  /admin/v1/users → 返回 PG 中的账号（phone_mask 已脱敏）
  /admin/v1/logout → 令牌失效，再用返回 401
```

## 四、部署规则（写死）

| 环境 | DATABASE_URL | REDIS_URL | 行为 |
|---|---|---|---|
| 本地开发 | 空 | 空 | 内存（仅此一种情况允许） |
| 生产 | **必须有** | **必须有** | 缺失即拒绝启动 |

## 五、剩余项

1. 限流/风控计数仍在进程内 —— 与后台会话同源问题，同样要搬 Redis。
2. TOTP 重放防护、secret base32 解码。
3. 采集/编解码只有 trait 与假实现。
4. Flutter 客户端未开始。
