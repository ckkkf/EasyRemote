# attach 错误结构化与 UI 脱敏（ER-905 4.1(4)）

## 原缺陷

```rust
// er-client::Gateway::call（修复前）
message: format!("request failed: HTTP {status} {method} {url} body={body:?}")
```

内部 URL + 原始 body 被拼进 `ServiceError.message`，经 FFI 原样传到 Flutter，
直接显示在画面中央。同时 `ServiceError` 只有 `code/message`，
信封里的 `name/retryable/trace_id` 全部丢弃。

## 修复

### 1) `ServiceError` 结构化（`core/crates/er-client/src/lib.rs`）

```rust
pub struct ServiceError {
    pub code: String,      // 数字码；NET/PARSE/HTTP 为稳定分类
    pub name: String,      // 枚举名
    pub message: String,   // 面向用户，禁止含 URL/body/凭证
    pub retryable: bool,
    pub trace_id: String,
    pub status: u16,
}
```

- 删掉 `ErrorBody { code: String }`：**服务端 `code` 是数字**，用它反序列化必失败，
  这正是「解析失败 → 回退到拼接 raw body」的根因。
- 统一走 `serde_json::Value`：`code` 接受 number/string，再 `to_string()`。
- HTTP 错误分支：优先解析信封；解析不出则返回稳定文案 `请求失败（HTTP {status}）`。
- 内部上下文只进 `eprintln!`，且只含 method/path/status/code/name/trace/body_len，
  不含 URL 查询串、不含 body 内容、不含 token/device_token。

### 2) FFI 返回结构化错误（`core/crates/er-ffi/src/session.rs`）

```json
{"ok":false,"error":{"code":"300006","name":"SESSION_AUTH_WINDOW_EXCEEDED",
  "message":"本次授权已到期，请重新连接","retryable":false,"trace_id":"..."}}
```

### 3) Dart 结构化解析 + 可重试 UI

- `session_video_io.dart` 新增 `AttachFailure{code,name,message,retryable,traceId}`，
  `attach` 失败时保存 `failure`；`display` 只返回 message。
- `session_page.dart` 占位区：显示 message + `Key('attach-error-copy-trace')` 的**复制 trace_id** 按钮；
  不再展示 URL 或原始 JSON。
- `_reconnectVideo`：复位 `_videoStarted = false` 后重新 `_startVideoOnce()`，
  并用 `_reconnecting` 挡住并发点击 —— 之前只清 timer/FFI，标志仍为 true，按钮是死键。

## 测试

```
$ cargo test -p er-client --lib error_envelope
service_error_keeps_envelope_fields_without_internal_url  PASS
internal_errors_are_classified_and_clean                   PASS
$ cargo test -p er-client --lib
34 passed

$ flutter test test/session_lifecycle_test.dart test/session_exit_routes_test.dart test/session_close_coordinator_test.dart
00:00 +12: All tests passed!
```

## 跨端回归

```
$ OUT=/tmp/er905-e2e-r7 bash scripts/mac/e2e_lifecycle.sh
E2E[A]_PASS converged=0s
E2E[B]_PASS PROBE_PEER_CLOSED reason=local_disconnect
E2E[C]_PASS rejected
E2E_ALL_PASS
```

## Gate

`33-gate-attach-error.txt`：`EXIT=0`，`GATE: PASS`（一次 FAIL 因未 `cargo fmt`，已格式化后重跑）。

## 未完成（如实）

- Flutter 侧「UI 不泄露 URL/body」「trace_id 可复制」「重试会再次 attach 且不并发」三条**尚无独立 widget 测试**；
  当前只有 Rust 侧字段断言 + Dart 静态分析 + 代码路径改动。补测试为下一步。
