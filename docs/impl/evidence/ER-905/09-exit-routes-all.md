# 里程碑 2e：退出入口全覆盖（ER-905）

## 监督意见与处理

| 意见 | 处理 |
|---|---|
| 测试 wrap 未传 `probeNat: false`，pending Timer 来自 NetProbe | wrap 现显式传 `probeNat: false`；另加 `pollSessionStatus: false`、`enableVideo: false`，彻底不启定时器/轮询/FFI |
| 禁止靠 pump 固定等待 / 扩大 timeout | 测试不再依赖 NetProbe 完成，改用注入；`pump()` 只推一帧渲染，无 `Duration` 等待 |
| 未覆盖 live 顶栏 `back-to-devices-live` | 新增 `TestLiveBackButtonClosesBeforeLeaving`（用 `initialStatus: SessionStatus(active)` 注入 live 态） |
| 需可注入初始状态/视频 adapter，不触发真实 FFI | `SessionPage` 新增 `initialStatus` / `pollSessionStatus` / `enableVideo` 三个注入点（生产默认保持原行为） |
| 保留系统返回断言 | 新增 `TestSystemBackClosesBeforeLeaving`，用 `tester.binding.handlePopRoute()` 触发 PopScope |
| 新增测试先独立跑绿再进 gate | 先单文件跑：4/4 PASS；再跑 gate：20/20 PASS |

## 注入点（生产默认不变）

```dart
SessionPage({
  ...
  this.probeNat = true,            // NAT 探测定时器
  this.initialStatus,              // 测试注入初始领域状态
  this.pollSessionStatus = true,   // 状态轮询
  this.enableVideo = true,         // FFI 视频接管
})
```

## 四个入口的断言（真 widget + MockClient）

```
$ flutter test test/session_exit_routes_test.dart
TestBackToDevicesClosesBeforeLeaving   PASS  点击 back-to-devices      -> /v1/sessions/close 先发生，onLeave 1 次
TestCancelConnectClosesBeforeLeaving   PASS  点击 cancel-connect      -> 同上
TestLiveBackButtonClosesBeforeLeaving  PASS  点击 back-to-devices-live -> 同上
TestSystemBackClosesBeforeLeaving      PASS  handlePopRoute()         -> 同上（PopScope）
00:00 +4: All tests passed!
```

判定方式：`calls.any((c) => c.contains('/v1/sessions/close'))` + `leaveCalls == 1`。
入口若绕过 coordinator，两条断言同时失败。

## Gate

- 完整输出：`08-gate-full-output.txt`（`EXIT=0`，20/20，`GATE: PASS`）
- 第 17 步 `flutter test` 含本次新增的 4 条退出路径测试，结果 PASS
