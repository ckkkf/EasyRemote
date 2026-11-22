# ER-110 证据：生产 ConsentNotifier 与跨实例通知

- 状态: 完成（代码与跨实例 Redis 测试；gate PASS）
- 限制：两个 signal 实例在同一测试进程内，通过真实 Redis Pub/Sub 传输；独立进程矩阵归 ER-903/ER-900。

## 1. 交付

| 文件 | 目的 |
|---|---|
| server/internal/signal/consent_notifier.go | 生产通知器：SaveBinding + Publish TypeConsentRequired |
| server/internal/stack/build.go | BuildGateway 增加 notifier 参数，显式注入 |
| server/internal/stack/stack_pg.go | NewWithStoreAndSessions/NewPostgresWithRedis 接收 notifier |
| server/cmd/gateway/main.go | REDIS_URL 时创建 RedisBus 并注入；不再依赖 devHub |
| server/cmd/stack/main.go | PG+Redis 分支创建 Bus/Hub、订阅、挂 rendezvous、注入 notifier |
| signal 测试 | fake bus 单测 + Docker Redis 两实例测试 |

## 2. 路由语义

- 网关：notifier.NotifyConsentRequired(session, from, to) → Bus.SaveBinding → Bus.Publish(to 频道)。
- 实例 B：RedisBus.Subscribe → hubB.DeliverLocal(deviceID, Envelope) → 只投给本机目标设备。
- 实例 A 的订阅收到同一消息但本地无该设备 → 忽略。
- 绑定跨实例可读，供被控端回传 consent 时使用。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 通知走 Bus 且写入绑定 | TestConsentNotifierUsesBus | PASS |
| 两实例跨 Redis 送达 | TestConsentNotifierCrossesSignalInstances | PASS |
| 绑定可被另一实例读取 | 同上（LoadBinding） | PASS |

真实 Redis 由测试用 docker 启动（redis:7-alpine，随机本地端口），结束自动删除。

## 4. 命令与输出

    $ cd server && go test ./internal/signal/ -run TestConsentNotifier -v
    --- PASS: TestConsentNotifierCrossesSignalInstances
    --- PASS: TestConsentNotifierUsesBus
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 尚未用两个独立 OS 进程验证 gateway+rendezvous；需要共享 PG 才能跨进程绑定设备。完整矩阵归 ER-903（chaos/多实例）与 ER-900（桌面 E2E）。
- 目标离线/投递失败仍由 ER-111 改成请求 fail-closed。
- devHub 仍是内存一体化开发的 notifier；生产 cmd/gateway/PG+Redis 已显式使用 Bus。

## 6. 回滚

- 回滚 BusNotifier 会回到“网关不通知/进程内通知”，属于 A-01 退化，禁止。
- Bus 故障时应返回错误并由 ER-111 转成明确失败，不得静默等待。
