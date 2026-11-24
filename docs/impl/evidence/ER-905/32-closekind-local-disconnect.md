# CloseKind：local_disconnect 归属修正（round 10）

## 漏项

`closekind.go` 上一轮加了「用户主动断开不计异常」的注释，但**没有显式映射 `local_disconnect`**。
它随后被 `strings.Contains(r, "disconnect")` 匹配为 `ClosePeerOffline`，
而 `abnormalKinds` 包含 `peer_offline` —— 于是「被控端用户自己点断开」被统计成**异常中断**，
与注释和真实语义同时矛盾。

## 修复

```go
case r == "local_disconnect":
    return CloseUserHangup     // 发起方是用户本人
case r == "peer_closed":
    return ClosePeerOffline
```

位置在通用 `contains` 规则之前，避免被 `disconnect` 抢走。

## 两个观察点不冲突（注释已写明）

| 观察点 | `local_disconnect` 的含义 | 期望 |
|---|---|---|
| 服务端统计 `CloseKind` | 被控端本机用户点断开 | `user_hangup`（不计异常） |
| 主控端 UI `SessionCloseReason` | 对端结束了控制 | `peerClosed`（显示「对方已结束远程控制」） |

## 测试

`TestClassifyCloseCoversEr905Reasons` 增两处断言：

```go
{"local_disconnect", CloseUserHangup},
if abnormalKinds[ClassifyClose("local_disconnect")] {
    t.Fatal("被控端本地断开是用户主动行为，不应计入异常中断")
}
```

```
$ cd server && go test ./internal/session -run 'ClassifyClose' -v
--- PASS: TestClassifyCloseCoversEr905Reasons
--- PASS: TestClassifyCloseKinds

$ cd server && go test ./internal/session ./internal/gateway
ok  session  0.199s
ok  gateway  0.496s
```

## Gate

`31-gate-closekind-local.txt`：`EXIT=0`，`GATE: PASS`。
