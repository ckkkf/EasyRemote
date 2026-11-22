# ER-112 证据：presence 租约

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 变更

| 文件 | 目的 |
|---|---|
| signal/hub.go | 新增 TypeHeartbeat=0x7F；Hub.Touch 续租（只接受当前 conn） |
| stack/rendezvous.go / cmd/rendezvous/main.go | 收到心跳调用 hub.Touch |
| gateway/handler.go | /v1/devices 在注入 Presence 时以租约为准 |
| signal/presence_test.go | 可控时钟 ttlBus：续租/断开/崩溃/旧连接 |
| gateway/presence_view_test.go | 设备列表在线状态来源 |

## 2. 语义

- 注册：Bus.SetOnline(dev, "instance#gen", 5min)。
- 心跳（客户端 20s 一次，服务端 15s 建议）：Touch → 同一 token 重新 SetOnline。
- 断开：Unregister(dev, self) 匹配当前 conn 后 ClearOnline(token)。
- 崩溃：无心跳，TTL 到期后 IsOnline=false。
- 旧连接：conn 身份不匹配 → Touch=false，不会替新连接续命。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 心跳续租 TTL | TestPresenceRefreshesOnHeartbeat | PASS |
| 断开立即离线 | TestDisconnectOffline | PASS |
| 崩溃后 TTL 过期离线 | TestCrashExpires | PASS |
| 旧连接不能续租 | TestTouchRejectsStaleConn | PASS |
| 设备视图读 Presence | TestDeviceOnlineReadsPresence | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/signal/ -run 'TestPresence|TestDisconnect|TestCrash|TestTouch' -v
    PASS
    $ go test ./internal/gateway/ -run TestDeviceOnlineReadsPresence -v
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 后台设备/概览在线数仍读 store（ER-800 统一为权威 presence）。
- 现有 Redis 集成测试（bus_test）在无 REDIS_URL 时 skip；本卡的租约测试用可控时钟替身，不依赖 Redis。
- presenceTTL=5min、客户端心跳 20s；弱网长期丢心跳会误离线，ER-903 chaos 验证。

## 6. 回滚

- 回滚心跳续租会重现“注册 5 分钟后在线设备被判离线”和长连接被服务端断开，禁止。
- 回滚设备视图会让 UI 继续显示绑定表的历史 online，禁止。
