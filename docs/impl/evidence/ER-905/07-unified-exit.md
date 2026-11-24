# 里程碑 2d：统一退出入口（ER-905）

## 监督指出的绕过点（全部修复）

| 位置 | 原实现 | 现在 |
|---|---|---|
| waiting 顶部「返回设备列表」（key `back-to-devices`） | `onPressed: widget.onLeave` | `_disconnectAndLeave(reason: 'controller_cancel')` |
| 取消连接（key `cancel-connect`） | `_leaveWith('已取消本次连接请求')` | `_disconnectAndLeave(reason: 'controller_cancel')` |
| live 顶栏返回（key `back-to-devices-live`） | `onPressed: widget.onLeave` | `_disconnectAndLeave()` |
| 系统返回（Esc/手势） | 无拦截 | `PopScope(canPop: false, onPopInvokedWithResult: → _disconnectAndLeave(reason: 'controller_cancel'))` |

现在 `widget.onLeave` 在 `session_page.dart` 中只剩 **1 处**——`_leaveWith()` 内部，
也就是唯一出口，且 `_leaveWith` 只由 coordinator 的 `leave` 回调触发。

## 关闭进行中：可见 pending，不静默吞

- `_disconnectAndLeave` 在 `_closer.closing` 时弹出「正在断开，请稍候…」并 return（不再静默）。
- 断开按钮 `_ToolItem.busy` 为 true 时禁用并显示「断开中…」。
- 协调器 `disconnect` 对并发点击返回 `CloseOutcome.busy`（单测覆盖）。

## 新增 widget 测试（真 widget + MockClient）

```
$ flutter test test/session_exit_routes_test.dart
TestBackToDevicesClosesBeforeLeaving   PASS  点击返回 → 先 /v1/sessions/close，再 onLeave(1 次)
TestCancelConnectClosesBeforeLeaving   PASS  点击取消 → 同样先 close
```

断言方式：MockClient 记录请求路径，`expect(calls.any((c) => c.contains('/v1/sessions/close')), isTrue)`
——若入口绕过 coordinator，该断言直接失败。

## 顺带修的真实缺陷

`_probeNat()` 在 `initState` 无条件启动 NetProbe 的 2s 定时器；widget 测试暴露为
`Pending timers`。已加注入开关 `SessionPage(probeNat: false)` 供测试关闭（生产默认 true，行为不变）。

## Gate

- 完整输出：`06-gate-full-output.txt`（20/20，`EXIT=0`，`GATE: PASS`）
- 期间一次 FAIL 由 `unused_import`（warning）引起，已删除该 import。

## 仍未完成

- 被控端 `device_list_page._disconnectRemote`（现象 3）。
- Rust `host_stream` close 收敛。
- 跨进程 E2E。
