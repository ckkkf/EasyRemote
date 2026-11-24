# 里程碑 5：worker isolate 注入与 FFI 释放（ER-905）

## 监督意见处理

### 1) 注入值跨 isolate 可见性（已修，实测绿）

```dart
Future<HostCloseResult> closeSessionInWorker(String hostId, String sessionId) {
  // spawn 之前读进局部变量：closure 里直接读顶层 `_closeWorker` 在新 isolate 里恒为 null
  final worker = _closeWorker;
  return Isolate.run(() {
    if (worker != null) return worker(hostId, sessionId);
    return _closeInNewIsolate(hostId, sessionId);
  });
}
```

- 注入值必须是**可发送的顶层函数 tear-off**（测试用 `_blockingWorker` / `_throwingWorker`）。
- `host_service_stub.dart` 提供同形 `closeWorker` setter（否则 analyze `undefined_identifier`）。
- 测试用 `tearDown(() => closeWorker = null)` 复位。

```
$ flutter test test/host_close_worker_test.dart
TestHostCloseDoesNotBlockUiHeartbeat        PASS   同步 200ms worker 在 isolate 内，心跳 beats>3
TestHostCloseWorkerFailureSurfacesToCaller  PASS   异常冒泡给调用方
00:00 +2: All tests passed!
```

### 2) FFI 字符串释放（已修）

`_closeInNewIsolate` 内 `lookupFunction('er_free_string')` 并在 `finally` 释放返回指针；
`lifecycle_probe.dart` 新增 `callWithFree`，attach 的 6 个参数与 info 的每轮参数全部 `calloc.free`。

### 3) UI 异常路径（已修）

`_disconnectRemote` 用 `try/catch` 把 worker 异常转成 `HostCloseResult(ok:false, error:e)`，
`finally` 语义由 `setState(_hostClosing = false)` 保证；按钮不会永久停在「断开中…」。

### 4) Rust 测试命名（部分未达）

`peer_close_clears_host_stats_within_deadline` 当前只断言 `peer_closed()` 谓词，
**没有测 deadline，也没测 stats 清理** —— 名不副实，需改名为 `peer_closed_state_predicate`，
另建真实收敛测试。已记录为未完成项（见下）。

### 5) E2E 脚本（已重写，未跑通）

`scripts/mac/e2e_lifecycle.sh` 已改为：trap 清理、只杀自己的 PID、随机空闲端口、
自起 stack+relay、readiness probe（轮询不用固定 sleep）、临时 `$HOME`、不写密码/token 到日志。
**本次运行未通过**：`rm -rf $TMP_HOME` 与 go build 缓存冲突（临时 HOME 让 GOCACHE 落进 TMP_HOME）。
需要把 go build 移出临时 HOME（或单独设 GOCACHE），属未完成项。

### 6) E2E 三条路径（未达）

当前脚本只覆盖「主控主动断开」；拒绝、被控端断开两条未覆盖。
按监督要求：**本卡保持 beta，不标完成**。

## Gate

- `19-gate-round5.txt`：`EXIT=0`，`GATE: PASS`
