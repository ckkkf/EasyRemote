# 里程碑 2：客户端会话生命周期（ER-905）

## 改动

| 文件 | 改动 | 对应现象 |
|---|---|---|
| client/lib/api.dart | sessionStatus 新增 closed → SessionState.ended；reason 走分类；新增 closeSession（GW-25） | 现象 1/4 |
| client/lib/models.dart | 新增 SessionCloseReason（user_rejected/consent_timeout/peer_closed/closed_by_self）+ SessionStatus.reason | 现象 4 |
| client/lib/pages/session_page.dart | initState 不再 attach；改为 _pollOnce 首次见到 authorized/active 时 _startVideoOnce()（幂等）；断开按钮改走 _disconnectAndLeave()（先 GW-25，确认 closed 才离开；失败留页面并展示 trace_id） | 现象 1/2 |
| client/test/session_lifecycle_test.dart（新增） | TestClosedStateReasonMapping、TestSessionStatusCarriesReasonAndEndedIsTerminal | 现象 4 |

## 证据

```
$ flutter test test/session_lifecycle_test.dart
00:00 +2: All tests passed!

$ bash scripts/gate.sh
GATE: PASS
```

## 未完成（下一里程碑）

- device_list_page 的 _disconnectRemote 仍忽略 close 结果（现象 3）。
- Rust host_stream 需在 close 事件下 deadline 内清 stats.session_id（现象 2/3 的数据面侧）。
- 跨进程 E2E：TestRemoteLifecycleRejectAndBidirectionalDisconnect。
- Dart 注入式测试（TestSessionPageDoesNotAttachBeforeAuthorized 等）需要把 GatewayClient/attach 抽象成可注入 adapter。
- GW-11 错误语义（ER-905-SPEC-01 blocker，不改 spec）。
