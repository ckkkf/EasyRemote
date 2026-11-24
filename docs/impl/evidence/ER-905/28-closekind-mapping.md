# CloseKind / reason 口径修正（ER-905 衍生缺陷）

## 发现

`ClassifyClose` 只认「含 timeout/expired/offline/disconnect/hangup 的英文短语」，
而 ER-905 引入的 reason 是**下划线机器串**，全部落到 `CloseUnknown`：

```
auth_window_exceeded  -> unknown
user_rejected         -> unknown
controller_closed     -> unknown
peer_closed           -> unknown
consent_timeout       -> timeout     (碰巧命中 contains "timeout")
request_expired       -> timeout     (碰巧命中 contains "expired")
```

`unknown` 在 `abnormalKinds` 里 = **异常中断**。后果：
用户主动拒绝、用户主动断开、授权窗口到期这些**正常/可预期**的结束，
都会被后台「异常中断率」算成故障，指标失真。

## 修复

在 `closekind.go` 的通用 contains 规则**之前**加显式映射（不新增枚举、不改 spec）：

| reason | CloseKind | 是否算异常 |
|---|---|---|
| `user_rejected` | `user_hangup` | 否（用户决策） |
| `controller_closed` / `host_closed` / `closed_by_self` | `user_hangup` | 否（用户主动断开） |
| `peer_closed` | `peer_offline` | 是（对端离开） |
| `auth_window_exceeded` | `timeout` | 是（授权窗口到期） |
| `consent_timeout` / `request_expired` | `timeout` | 是 |

## 测试

`server/internal/session/closekind_er905_test.go::TestClassifyCloseCoversEr905Reasons`

```
$ cd server && go test ./internal/session -run 'ClassifyClose' -v
--- PASS: TestClassifyCloseCoversEr905Reasons
--- PASS: TestClassifyCloseKinds
```

不变量断言（守语义而非实现细节）：
- `user_rejected` / `controller_closed` **不得**计入异常中断；
- `peer_closed` **必须**计入异常中断。

## Gate

`27-gate-closekind.txt`：`EXIT=0`，`GATE: PASS`。
