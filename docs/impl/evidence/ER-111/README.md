# ER-111 证据：请求 fail-closed

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 行为

- 建会话前（扣减验证码/密码之后）：
  - Presence 非空 → Online(target)；false 或错误 → CONN_DEVICE_OFFLINE，不建会话。
- 建会话后：
  - notifier 缺失且 Presence 非空 → Close(session) + CONN_DEVICE_OFFLINE；
  - notify 返回错误且 Presence 非空 → Close(session) + CONN_DEVICE_OFFLINE；
  - Presence 为 nil（内存开发）→ 保持旧行为，仅供本地/测试。
- 成功路径仍返回 requested，由授权事件推进。

## 2. 装配

- cmd/gateway：REDIS_URL → RedisBus + ConsentNotifier + Presence；生产缺 REDIS_URL 直接退出。
- cmd/stack PG+Redis：Bus/Hub/Subscription/Rendezvous + Notifier + Presence；生产缺 REDIS_URL 退出。
- 内存 stack：Presence=nil，notifier=devHub（显式开发路径）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 目标离线不建会话 | TestConnectRequestFailsWhenTargetOffline | PASS |
| notifier 缺失时失败并清理 | TestConnectRequestFailsWhenNotifierUnavailable | PASS |
| 投递失败时失败并清理 | TestConnectRequestFailsWhenDeliveryFails | PASS |
| 在线+投递成功正常 requested | TestConnectRequestSucceedsWhenOnlineAndNotified | PASS |

测试同时断言 ListSessions(requested) == 0，防止僵尸会话。

## 4. 命令与输出

    $ cd server && go test ./internal/gateway/ -run TestConnectRequest -v
    4 passed
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- Presence 的租约/心跳续租仍是 ER-112；当前 Bus.IsOnline 只反映注册 token 是否存在。
- 生产未配置 Redis 时启动失败；开发/内存环境仍是显式 fail-open，不得用于发布。
- 连接请求错误目前统一为 CONN_DEVICE_OFFLINE；更细的“notifier 不可用”可重试错误待 ER-113/错误码细化。

## 6. 回滚

- 回滚会重新返回假 requested，禁止。
- 回滚生产启动校验会允许无 Bus 部署，禁止。
