# 第四轮：Store 接口重构 + PostgreSQL 实现

> 日期：2026-11-20 ｜ 承接 [review-round3.md](./review-round3.md)

## 一、Store 接口重构（审查项 S10）

| 变更 | 之前 | 现在 |
|---|---|---|
| 上下文 | 无 `context`，慢查询无法取消 | 所有方法带 `ctx` |
| 读取语义 | `(T, bool)`，"不存在" 与 "DB 故障" 无法区分 | `(T, error)`，`ErrNotFound` 独立哨兵 |
| 列表 | 依赖类型断言 `AllAccounts()`，PG 下静默返回空 | `ListAccounts/ListDevices/ListSessions` 进接口，带分页 |
| 唯一键冲突 | 无统一表达 | `ErrConflict` |

**为什么重要**：旧接口下数据库挂了会被渲染成"账号不存在"，客服和风控都会被误导，而且 `700002 SRV_DB_UNAVAILABLE` 永远发不出来。

## 二、PostgreSQL 实现

`server/internal/store/pg.go`（pgx v5 + pgxpool）：

| 能力 | 实现 |
|---|---|
| 连接池 | `pgxpool`，启动时 `Ping` 校验 |
| 迁移 | `migrations/001_init.sql` 幂等建表；`cmd/stack` 启动自动执行 |
| 错误映射 | `pgx.ErrNoRows → ErrNotFound`；`23505 → ErrConflict`；其余原样返回 |
| 验证码原子性 | **单条 UPDATE 完成消费**；错误路径单条 UPDATE 自增并在阈值处设 `locked_till` |
| 审计只追加 | 建表脚本带触发器，UPDATE/DELETE 直接 RAISE EXCEPTION |
| 分页 | `LIMIT/OFFSET`，不再全表返回 |

## 三、契约测试：同一套用例跑两种实现

`internal/store/storetest` 定义 10 组契约用例，`Memory` 与 `PG` 各跑一遍：

```
AccountLifecycle · AccountUniquePhone · DeviceLifecycle · DeviceCodeUnique
VerifyCodeAtomic · TrustExpiry · SessionLifecycle · AuditOrder
Pagination · NotFoundIsDistinct
```

这直接回答了审查里那句"测试绿是假象"——现在两种实现的语义由同一套断言钉住。

## 四、验证证据

| 项 | 结果 |
|---|---|
| 错误码三端一致 | ✔ |
| gofmt / vet | 无输出 |
| Go 单测 | 14 包全绿 |
| Go 竞态 | 14 包全绿 |
| 集成（内存） | 通过 |
| **集成（真实 PostgreSQL 15）** | **TestPGContract 10/10 通过** |
| Rust | 35 passed |
| Web 构建 | 成功 |

真实 PG 冒烟（`cmd/stack` + 本机 pgsql_server）：

```
healthz → 200
登录 + 绑设备 → 返回 device_code / device_token
伪造后台令牌 → 401
PG 内数据核对 → accounts=1, devices=1
```

## 五、环境说明

本机原本已有 `pgsql_server`（postgres:15-alpine，端口 5432，已运行 10 天）。我第一次检查时只看了 `psql/postgres` 命令是否存在（均未安装），误判为"没有本地 PG"，于是起了一个 `er-pg` 容器（55432）。后来发现已有实例，已删除 `er-pg`，测试改用 `pgsql_server` 上的独立库 `easyremote_test`。

## 六、仍未完成

1. 后台会话（`adminsession`）仍是进程内存表，多实例部署需要搬到 Redis —— 本机已有 `redis_server` 可用。
2. TOTP 重放防护、secret base32 解码。
3. 采集/编解码只有 trait 与假实现。
4. Flutter 客户端未开始。
