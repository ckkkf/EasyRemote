# 里程碑 2c：监督纠偏二（ER-905）

## 纠偏点

1. `SessionState.failed` 被当成终态直接本地离开 —— 仍不安全：failed 可能只是 status/网络/attach/UI 失败，
   并不证明服务端 `closed`。本页持有 `session_id`，因此**只有 `ended` 可以跳过 GW-25**。
2. `catch(e).toString()` 丢 `trace_id`（`ApiException.toString() => message` 只返回 message），
   用户无法把凭据交给客服。
3. 证据里只写 `GATE: PASS` 不够：要保存完整 gate 输出文件，并区分存量 lint 与新增问题。

## 改法

`client/lib/session_close_coordinator.dart`
- `disconnect(state)`：仅 `SessionState.ended` 直接 `leave()` 返回 `alreadyEnded`；
  `requested / authorized / active / failed / idle` 一律先 `closeRemote()`（GW-25）。
- 新增结构化 `CloseError{message, code, traceId}`；捕获 `ApiException` 时保留 `name/code/traceId`，
  不再用 `toString()`。`CloseError.display` 输出「message（trace_id xxx）」。
- 未确认 `ended` 时记 `SESSION_CLOSE_NOT_CONFIRMED`，不释放、不离开。

`client/lib/pages/session_page.dart`
- 失败提示改用 `CloseError.display`，并在 SnackBar 提供「复制 trace_id」动作（8 秒，可重试）。
- 仍不改写 `_status`（保留原领域状态）。

## 测试（全部通过）

```
$ flutter test test/session_close_coordinator_test.dart
TestRequestedCancelCallsCloseBeforeLeaving   PASS  requested → closeCalls=1, release=1, leave=1
TestFailedStateStillCallsClose              PASS  failed    → closeCalls=1
TestEndedStateSkipsCloseAndLeavesDirectly   PASS  ended     → closeCalls=0
TestActiveCloseFailureKeepsStateAndAllowsRetry PASS active 失败 → release=0, leave=0,
                                                  error.traceId=trace-abc123, 重试后 closeCalls=2
TestRepeatedClicksSendCloseOnce              PASS  并发点击 → 第二次 busy，closeCalls=1
TestCloseNotConfirmedDoesNotRelease          PASS  服务端返回 authorized → 不释放
00:00 +6: All tests passed!
```

## Gate

- 完整输出：`04-gate-full-output.txt`（20/20 步，汇总 `GATE: PASS`，退出码 0）
- lint 明细：`04b-lint-detail.txt`
  - 闸门第 16 步只要求 **error/warning = 0**；当前 analysis 输出全为 `info`（存量 124 条，
    例如 `deprecated_member_use`、`prefer_interpolation_to_compose_strings`），
    本卡新增文件未引入新的 info（coordinator/session_page 相关条目已在基线内）。

## 仍未完成

- 被控端 `device_list_page._disconnectRemote`（现象 3）。
- Rust `host_stream` close 收敛。
- 跨进程 E2E。
