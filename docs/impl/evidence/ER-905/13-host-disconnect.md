# 里程碑 3：被控端断开（现象 3，ER-905）

## 根因（代码证据）

```dart
// device_list_page.dart（修复前）
Future<void> _disconnectRemote() async {
  final id = _hostStatus.sessionId;
  if (id.isNotEmpty) HostService.instance.closeSession(id);   // 返回值 bool 被丢弃
  setState(() => _hostStatus = HostStatus.idle);              // 乐观清 UI
}
```

- `HostService.closeSession` 只返回 `bool`，FFI 里的 `{ok, error}` 被压平，错误原因丢失。
- 服务端未真正 closed 时，2 秒后的 FFI 轮询把旧 `session_id` 写回 → 控制条「消失又出现」。

## 改动

| 文件 | 改动 |
|---|---|
| `client/lib/host_service_io.dart` | `closeSession` 返回结构化 `HostCloseResult{ok, error}` |
| `client/lib/host_service_stub.dart` | 同形声明；补齐 `HostStatus.link` 字段（与 IO 版一致） |
| `client/lib/host_close_policy.dart`（新增，生产代码） | 断开判据的唯一来源：失败不清、成功才记已关闭、轮询不复活 |
| `client/lib/pages/device_list_page.dart` | `_disconnectRemote` 用 policy 结果驱动 UI；`_hostClosing` pending；失败在卡片内显示「断开失败：…（控制条保留，可重试）」；轮询对已关闭会话返回空 id |
| `client/test/host_close_policy_test.dart`（新增） | 测**生产类** `HostClosePolicy`（非测试内 fake） |

## 测试

```
$ flutter test test/host_close_policy_test.dart
TestLocalDisconnectFailureKeepsControlBar   PASS  失败 -> cleared=false, visibleSession 仍为该会话
TestLocalControlBarClearsAfterConfirmedClose PASS  成功 -> visibleSession 为空（轮询不复活）
TestCloseFailureWithoutReasonStillReports    PASS  无 error 时也有可读原因
00:00 +3: All tests passed!
```

## Gate

- `12-gate-host-disconnect.txt`：`EXIT=0`，`GATE: PASS`
- 期间两次 FAIL 均因接口形状：`HostStatus` 新增必填 `link` 未传；`host_service_stub.dart` 的 `HostStatus` 缺 `link` 字段（条件导入两版必须同形）。已修。

## 仍未完成

- Rust `host_stream`：**已确认**每 1 秒轮询 `session_state()`，`closed/failed/ended` 立即 break（见 15-e2e-lifecycle.md）。
- 跨进程 E2E。
