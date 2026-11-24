# attach 结构化错误传播（round 11，ER-905 4.1(4)）

## 监督指出的实质缺陷与处理

### 1) trace_id 仍会丢 —— 已改为真正结构化

根因：`ControllerAgent::begin/finish/mark_ready` 返回 `AgentError(String)`，
`ServiceError` 在那里就被 `e.to_string()` 压平，`AttachError::from(AgentError)` 只能拿到字符串，
`trace_id` 必然为空；而 `split(" body=")` 还会保留前面的内部 URL。

修复：新增**结构化变体**，不改动既有调用点：

```rust
ControllerAgent::begin_ex()      -> Result<..., ServiceError>   // relay_ticket 的 403 在这里
ControllerAgent::mark_ready_ex() -> Result<(), ServiceError>
TargetAgent::begin_ex() / mark_ready_ex()   // 被控端同形
```

`ctrl_session::attach` 改走 `*_ex`，`AttachError::from(ServiceError)` 直接搬 `code/name/message/retryable/trace_id`。
`From<AgentError>` 只剩**本地/传输**语义（`HANDSHAKE` / `AGENT`），**不再靠字符串猜 HTTP 状态**。

### 2) Gateway 解析与证据不一致 —— 抽 helper + 真测试

新增生产同源解析入口：

```rust
pub fn parse_error_envelope(raw: &str, status: u16) -> ServiceError
```

- `code` 接受 **number 或 string**（旧实现 `ErrorBody { code: String }` 对数字必然反序列化失败，
  这正是退化成「拼接原始 body/URL」的根因）；
- `name/message/retryable/trace_id` 逐字段取值，缺失给稳定默认；
- 非 JSON / 无 code → `请求失败（HTTP {status}）`，**不回显原始内容**；
- 生产 HTTP 分支改调该 helper，不再有第二套解析；
- `NET` 分支不再用 `e.to_string()` 当文案（ureq 的文本含完整 URL），改为稳定文案 + `eprintln!` 记录。

测试（`error_envelope_parser_tests`，全部走真实 helper，不是手工构造 ServiceError）：

```
parses_numeric_code_envelope        PASS   code=300006、name/trace_id 保留、message 无 URL/body
accepts_string_code                 PASS   code="CONN_RATE_LIMITED"、retryable=true
non_json_fallback_is_stable_and_clean PASS  502 HTML → 「请求失败（HTTP 502）」且不含 <html>
missing_fields_get_safe_defaults    PASS   只给 code 也有安全默认
```

### 3) SessionVideo 清理路径

- attach **成功**时清 `_failure` / `_error`；
- `catch (e)` 不再把原始异常当文案（可能含 URL/端口）：只进 `debugPrint`，UI 用稳定文案「连接失败，请检查网络后重试」；
- `close()` 在 `_ffiSession == null`（失败 attach 后）也清失败态并返回，重连时不会看到旧错误。

## 复验

```
$ cargo test -p er-client --lib
38 passed
$ cargo clippy -p er-client --all-targets -- -D warnings
(clean)

$ flutter analyze lib/session_video_io.dart lib/pages/session_page.dart
0 error

$ OUT=/tmp/er905-e2e-r8 bash scripts/mac/e2e_lifecycle.sh
E2E[A]_PASS converged=1s / E2E[B]_PASS local_disconnect / E2E[C]_PASS rejected / E2E_ALL_PASS

$ bash scripts/gate.sh
EXIT=0  GATE: PASS  (35-gate-structured-error.txt)
```

## 仍未完成（不标完成）

第 4 项要求的 4 条 Flutter 独立测试仍未写：

1. UI 不泄露 URL/body
2. trace_id 可见并能复制
3. 点击「重连」会再次 attach（覆盖 `_videoStarted` / `_reconnecting`）
4. 快速重复点击不并发

现有 `TestManualReconnectReattaches` 只直接调 `SessionVideo.attach` 两次，
不经过 `SessionPage` 的状态机，**不能算**这条验收。
依赖注入点已具备（`enableVideo` / `pollSessionStatus` / `initialStatus` / `SessionVideo.attachRunner`），
下一步补齐这 4 条后才能真正收口。
