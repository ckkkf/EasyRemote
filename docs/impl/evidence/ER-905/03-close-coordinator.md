# 里程碑 2b：监督纠偏（ER-905）

## 纠偏点

1. `!_status.isLive` 时本地离开 → 会在服务端遗留 requested 会话、在被控端遗留授权申请。
2. close 失败时把 `_status` 改成 `failed` → 丢失原领域状态，下一次点击又走本地离开。

## 改法

- 新增 `client/lib/session_close_coordinator.dart`（纯逻辑、可注入）：
  - 非终态（requested/authorized/active/idle）一律先 `closeRemote()`（GW-25），
    确认 `state=ended` 后才 `releaseLocal()` + `leave()`；
  - `ended/failed` 才允许直接离开，且**不调** GW-25；
  - 关闭失败：`releaseLocal`/`leave` 都不调用，`error` 记录原因，可重试；
  - `closing` 期间重复点击返回 `busy`。
- `session_page` 改用该协调器；失败时**不再**改写 `_status`，只显示错误与可重试提示；
  断开按钮在 `closing` 时禁用并显示「断开中…」（`_ToolItem.busy`）。

## 测试（红→绿）

```
$ flutter test test/session_close_coordinator_test.dart test/session_lifecycle_test.dart
TestRequestedCancelCallsCloseBeforeLeaving      PASS   ← requested 也调 GW-25，closeCalls==1
TestAuthorizedDisconnectCallsCloseOnce          PASS
TestEndedStateSkipsCloseAndLeavesDirectly       PASS   ← 终态不调 close
TestCloseFailureKeepsStateAndAllowsRetry        PASS   ← 失败不释放/不离开，重试后 closeCalls==2
TestCloseNotConfirmedDoesNotRelease             PASS   ← 服务端未确认 closed 也不释放
00:00 +7: All tests passed!
```

```
$ bash scripts/gate.sh
GATE: PASS
```

## 仍待完成

- 被控端 `device_list_page._disconnectRemote` 的 close 结果处理（现象 3）。
- Rust host_stream 在 close 下 deadline 内清 stats。
- 跨进程 E2E。
