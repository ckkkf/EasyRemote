# 里程碑 3b：监督纠偏（ER-905）

## 1) FFI 同步调用移出 UI isolate

- `HostService.closeSession` 内部是同步 ureq 网络请求；`Future` 包装不会让它非阻塞。
- 新增顶层 `closeSessionInWorker(hostId, sessionId)`：
  - 用 `Isolate.run` 在 worker isolate 里重新 `DynamicLibrary.open` 并调用 `er_host_close_session`；
  - `hostId/sessionId` 作为参数传入（单例字段不跨 isolate 共享）；
  - 提供 `closeWorker` 注入点供测试替换（默认 null → 真 isolate）。
- `device_list_page._disconnectRemote` 改为 `await closeSessionInWorker(...)`。
- Web stub 提供同形函数返回 `ok:false`（浏览器无 FFI）。

注：UI heartbeat 不被阻塞的**直接**测量需要真机 perf 采样；当前以「调用点不再同步执行 FFI」+ 单测注入为证据，
真实 heartbeat 指标列入未完成项（见下）。

## 2) 控制条 pending/error

`LocalControlBar` 新增 `busy` / `error`：
- `busy=true` → 按钮 `onPressed: null`、文案「断开中…」；
- `error` 非空 → 显示 `local-control-error` 文案「断开失败：…（可重试）」。
`DeviceListPage` 传 `busy: _hostClosing, error: _hostCloseError`。
重复点击不再静默：按钮已禁用（测试断言 `onPressed == null`）。

```
$ flutter test test/local_control_bar_busy_test.dart test/host_close_policy_test.dart
TestLocalControlBarShowsBusyAndDisablesButton PASS
TestLocalControlBarShowsCloseError             PASS
TestClosedTombstonesAreBounded                 PASS
00:00 +6: All tests passed!
```

## 3) 墓碑有界 + Rust 真收敛

- `HostClosePolicy` 墓碑改为有序列表，上限 `maxTombstones = 32`，超出淘汰最早一条；
  测试 `TestClosedTombstonesAreBounded` 验证最早条目被淘汰、规模有界。
- 墓碑只做**轮询去重**，不替代 Rust：`host_stream` 每 1 秒查 `session_state()`，
  `closed/failed/ended` 立即 `break` 并清 `stats.session_id`（`cargo test -p er-client` 31 passed）。

## 4) 文档修正

`13-host-disconnect.md` 中「当前靠心跳 10s」改为「已确认每 1 秒轮询 `session_state()`」。

## 5) 固定 sleep（blocker 证据）

`host_stream.rs` 仍有 `1200ms` / `2500ms` 两处固定等待，用于「consent 后等对端注册中继」。
<br>不可规避原因：中继注册是 **UDP**，协议层没有 ACK；`RelayClient::announce` 无法得知
对端何时可收包。要移除固定等待，必须新增中继层确认消息（属于 `docs/spec/protocol.md` 变更）。

- blocker id：`ER-905-PROTO-01`（relay 注册确认）
- 现状：两处 sleep 已集中在一处并带注释，本卡不擅自改协议

## 仍未完成

- UI heartbeat 真实测量（需真机 perf 采样）。
- 真实双进程 Rust 客户端 E2E。
- `ER-905-SPEC-01`、`ER-905-PROTO-01` 两个 blocker 待批准。
