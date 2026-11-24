# round 13：真实 widget 测试 + attach 全链路结构化（ER-905 4.1(4)）

## 1) attach 的三个网关卡点全部结构化（trace_id 不丢）

| 卡点 | 之前 | 现在 |
|---|---|---|
| `begin`（relay_ticket 403） | `AgentError(String)` | `begin_ex() -> ServiceError` |
| `finish`（内部**再取一次** ticket） | `AgentError` | `finish_ex() -> ServiceError` |
| `mark_ready` | `AgentError` | `mark_ready_ex() -> ServiceError` |

`ctrl_session::attach` 与 `connect` 两条路径都改走 `*_ex`；`connect` 的三轮重试保留**最后一次结构化原因**
（`last_err: Option<AttachError>`），不再压成字符串。
`From<AgentError>` 只剩本地/传输语义（`HANDSHAKE` / `AGENT`），**不再 `contains("HTTP 403")` 猜状态**。

## 2) 本地错误文案脱敏

- `announce` 失败：`eprintln!` 记录原文，UI 用「中继连接失败，请稍后重试」；
- `finish` 内 `wait_peer_handshake` / 公钥 / 验签失败：UI 用固定文案（「建立加密通道超时…」「对方公钥无效…」「对方身份校验失败…」）；
- `NET` 层：ureq 文本可能含完整 URL，改为「网络连接失败，请检查网络后重试」+ stderr。

## 3) 生产同源错误解析 + 真测试

```rust
pub fn parse_error_envelope(raw: &str, status: u16) -> ServiceError
```

- `code` 接受 number / string（旧 `ErrorBody{code:String}` 对 `300006` 必然失败，这是泄露 raw body 的根因）；
- 生产 HTTP 分支直接调它；非 JSON → 「请求失败（HTTP {status}）」。

```
$ cargo test -p er-client --lib error_envelope
parses_numeric_code_envelope          PASS
accepts_string_code                   PASS
non_json_fallback_is_stable_and_clean PASS
missing_fields_get_safe_defaults      PASS
service_error_keeps_envelope_fields_without_internal_url PASS
internal_errors_are_classified_and_clean                 PASS
$ cargo test -p er-client --lib  → 38 passed
```

## 4) 真正的 SessionPage widget 测试（不是读单例）

新增 `client/lib/video_adapter.dart`（`VideoAdapter` 抽象 + `DefaultVideoAdapter` 委托单例），
`SessionPage(video: ...)` 注入 fake，**不加载 FFI**。

`client/test/session_page_attach_ui_test.dart`（pump MaterialApp + SessionPage）：

```
TestSessionPageShowsSanitizedAttachError        PASS  取 Key('attach-error-message') 的 Text.data，
                                                     断言含「本次授权已到期」且不含 http:// / POST / body=
TestSessionPageCopiesTraceIdFromButton          PASS  tap Key('attach-error-copy-trace') 后，
                                                     mocked Clipboard 收到 tr-ui-copy
TestReconnectButtonAttachesAgain                PASS  tap Key('reconnect') 后 attachCalls +1
TestRapidReconnectDoesNotAttachConcurrently     PASS  Completer 卡住 attach，连点 3 次仍只有 1 次 in flight
00:00 +4: All tests passed!
```

并发保护来自 `_reconnecting`；重连生效来自 `_videoStarted = false` 复位。

## 5) SessionVideo 生命周期

- attach **开始**先清 `_failure/_error`；成功后清；
- legacy 字符串错误：原文进 `debugPrint`，UI 用「画面接管失败，请重试」；
- `catch (e)`：原文进日志，UI 用「连接失败，请检查网络后重试」；
- `close()`：`_ffiSession == null` 与正常分支**都**清 `_failure/_error`。

```
$ flutter test test/attach_error_structured_test.dart
TestAttachFailureKeepsStructuredFieldsWithoutInternalUrl PASS
TestLegacyStringErrorIsSanitized                          PASS
TestAttachSuccessClearsPreviousFailure                    PASS
```

## 6) 全量复验

```
$ flutter analyze lib            → 0 error / 0 warning（仅存量 info）
$ flutter test <10 个生命周期/错误测试文件>  → 31 passed
$ cargo test -p er-client --lib  → 38 passed
$ OUT=/tmp/er905-e2e-r10 bash scripts/mac/e2e_lifecycle.sh
E2E[A]_PASS converged=1s / E2E[B]_PASS local_disconnect / E2E[C]_PASS rejected / E2E_ALL_PASS
$ bash scripts/gate.sh → EXIT=0  GATE: PASS  (38-gate-round13.txt)
```

## 7) 仍存在的限制

- `ER-905-PROTO-01`：中继注册无 ACK，`host_stream`/`mac_host` 各两处固定等待（协议变更待批准）。
- `ER-905-SPEC-01`：GW-11 无「未授权/已拒绝」精确错误码。
- `ER-905-PROTO-02`：reason 缺 actor 前缀，`host_closed`/`controller_closed` 两端语义相反。
