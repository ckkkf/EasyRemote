# 第八轮：中继多节点 —— 最后一处进程内状态

> 日期：2026-11-20 ｜ 承接 [review-round7.md](./review-round7.md)

## 一、先做全仓审计

用 grep 扫所有进程内可变状态（`map` 字段、`sync.Mutex`、包级变量），逐个判定：

| 位置 | 性质 | 判定 |
|---|---|---|
| `adminsession.MemoryStore` | 进程内 | ✅ 仅测试用，生产走 Redis |
| `ratelimit.Memory` | 进程内 | ✅ 仅测试用，生产走 Redis |
| `adminapi.MemoryReplayGuard` | 进程内 | ✅ 仅开发用，生产走 Redis |
| `store.Memory` | 进程内 | ✅ 仅开发用，生产走 PG |
| `signal.Hub.devices` | 进程内 | ✅ 连接本身是局部的，绑定与在线表已共享 |
| **`relay.Forwarder.pairs`** | 进程内 | ❌ **多节点中继时两端注册到不同节点互不可达** |

只有一处真问题。

## 二、中继多节点方案

### 目录服务（Redis）

| key | 内容 | TTL |
|---|---|---|
| `er:relay:node:<nodeID>` | 该节点的 UDP 地址 | 30s（每 10s 心跳刷新） |
| `er:relay:route:<sid>:<dev>` | 该设备注册在哪个节点 | 2min |

### 新增帧类型

```
FrameNodeForward = 3
[type][u16 sidLen][sid][u16 devLen][targetDev][payload]
```

### 转发决策

`Forwarder.Plan()` 返回 `Route`：

1. 本地有对端连接 → `Route{Local: addr}`，直接投递；
2. 本地没有 → 查 `er:relay:route` 得到目标节点 → `Route{RemoteNode: nodeID}`；
3. 节点地址由 `er:relay:node` 查出，用 `FrameNodeForward` 转发过去；
4. 对端节点收到后按 `targetDev` 查本地连接投递。

**安全约束不变**：来源必须是已登记地址（`Plan` 先做 sender 校验），陌生人即使目录里有路由也发不进去。

## 三、验证

### 单元测试（4 条）

```
TestPlanPrefersLocalPeer              本地有对端时不下发跨节点
TestPlanFallsBackToRegistry           本地没有时按目录转发
TestPlanRejectsStrangerEvenWithRegistry  陌生来源即使有路由也拒绝
TestNodeForwardCodec                  跨节点帧编解码
```

### 双节点端到端（真实两个进程 + 真实 Redis）

```
A 注册到节点 1，B 注册到节点 2
  跨节点 A→B 送达              ✅ HELLO_FROM_A
  跨节点 B→A 送达              ✅ HELLO_FROM_B
  陌生人注入被丢弃              ✅ 未污染 B 的接收
```

### 全量

| 项 | 结果 |
|---|---|
| 错误码三端一致 | ✔ |
| Go 单测 | **16 包全绿** |
| Go 竞态 | 16 包全绿 |
| vet / gofmt | 无输出 |
| 集成（内存） | 通过 |
| PG 契约（真实 PostgreSQL 15） | 通过 |
| Redis 契约（会话/限流/信令/中继目录） | **4 个包全通过** |
| Rust | 35 passed |
| Web 构建 | 成功 |

## 四、多实例状态全景（现在全部共享）

| 状态 | 单实例 | 多实例 |
|---|---|---|
| 账号/设备/会话/验证码/审计 | 内存(dev) | PostgreSQL |
| 后台会话 | 内存(dev) | Redis |
| 限流计数 | 内存(dev) | Redis |
| TOTP 重放 | 内存(dev) | Redis |
| 信令在线表 | 进程内 | Redis |
| 信令会话绑定 | 进程内 | Redis |
| 信令消息投递 | 本地 | Redis Pub/Sub |
| 中继端位 | 进程内 | Redis 目录 + 节点间转发 |

## 五、生产部署拓扑（已验证的形态）

```
              ┌────────────┐
  客户端 ───▶ │  gateway    │──┐
             └────────────┘  │
             ┌────────────┐  ├──▶ PostgreSQL
        ───▶ │  rendezvous │──┤
             └────────────┘  ├──▶ Redis（会话/限流/重放/信令/中继目录）
             ┌────────────┐  │
        ───▶ │  relay ×N   │──┘
             └────────────┘
```
