# ER-905 远控会话生命周期回归修复

> 创建日期：2026-11-23
> 成熟度：`beta`（本卡通过跨进程 E2E 前不得升为 `stable`）
> 用户实测环境：Windows 主控端连接 macOS 被控端，本地开发网关 `http://127.0.0.1:18090`
> 本卡是当前远控回归的唯一执行卡。一次只修本卡列出的生命周期闭环，不混入管理端或其它功能改造。

## 1. 用户可见问题

1. 主控端进入会话后画面接管失败：`POST /v1/relay/ticket` 返回 HTTP 403，错误名为 `SESSION_AUTH_WINDOW_EXCEEDED`，但本次会话刚授权约一分钟，并非 12 小时授权窗口到期。
2. 主控端点击“断开连接”并离开后，被控端仍显示“正在被远程控制 · 会话 ...（仅本机可断开）”。
3. 被控端点击“断开远程控制”后，本地控制条仍然存在或稍后重新出现。
4. 被控端拒绝控制申请后，主控端没有显示“对方已拒绝”，仍停留在等待状态。

不允许把这四项分别做成 UI 补丁。它们共同说明 `requested -> authorized -> active -> closed` 的领域状态、数据面和两端 UI 没有使用同一权威闭环。

## 2. 字段与契约映射

本卡不新增或修改接口字段，不得修改 `docs/spec/contract.yaml` 或 `docs/spec/error-codes.md` 迁就实现。

| 链路 | 请求/响应字段 | 权威来源 |
|---|---|---|
| 发起连接 | `session_id`、`state`、`trusted` | `docs/spec/contract.yaml:366-370`，GW-06 `:748-753` |
| 本机授权 | `session_id`、`state`、`auth_expire_at` | `docs/spec/contract.yaml:371-374`，GW-07 `:754-759` |
| 查询状态 | `session_id`、`state`、`reason` | `docs/spec/contract.yaml:375-378`，GW-08 `:761-766` |
| 中继票据 | `ticket`、`self_device`；错误集 | GW-11 `docs/spec/contract.yaml:780-785` |
| 任一端关闭 | `POST /v1/sessions/close`；`session_id`、`state`、`reason` | GW-25 `docs/spec/contract.yaml:846-852` |
| 用户拒绝 | `CONN_CONSENT_REJECTED`，用户提示“对方拒绝了本次连接” | `docs/spec/error-codes.md:81` |
| 对端断开 | `SESSION_TERMINATED_BY_PEER`，用户提示“对方已结束远程控制” | `docs/spec/error-codes.md:106` |
| 授权窗口到期 | 仅真正超过 12h 时使用 `SESSION_AUTH_WINDOW_EXCEEDED` | `docs/spec/error-codes.md:109` |

若实现确实需要契约中不存在的新状态或错误码，停止本卡接口改动，另建 spec 变更卡并等待负责人批准。禁止在本卡直接扩大错误集。

## 3. 已确认的代码证据与待验证根因

以下是开工前必须用失败测试固化的证据，不得只根据描述直接改代码：

1. `client/lib/pages/session_page.dart` 在 `initState` 中无条件调用 `_startVideo()`；这可能在 `requested` 阶段就进入 Rust attach 并申请 `/v1/relay/ticket`。正确时序应是状态首次进入 `authorized` 后才启动一次 attach。
2. 同文件的主控端“断开连接”当前只调用 `_leaveWith('已断开连接')`，没有调用 GW-25，属于只退出 UI、不关闭领域会话。
3. `client/lib/pages/device_list_page.dart::_disconnectRemote` 忽略 `HostService.closeSession` 的 bool 结果，并立即把本地状态改为 idle；下一次 FFI 轮询可把旧 `session_id` 再写回来。
4. `client/lib/api.dart::sessionStatus` 没有把服务端 `closed` 映射为终态，并丢失了 `reason` 对 UI 语义的分类，导致拒绝/对端断开不能收敛为明确结果。
5. `server/internal/session` 当前拒绝路径会写入 `closed`，但同时返回 `CONN_CONSENT_REJECTED` error；`server/internal/gateway` 因而可能把一次合法的“拒绝决策”写成错误响应，与 GW-07 的 `ConsentResult` 完成态需要核对。
6. `/v1/relay/ticket` 当前把所有非 `authorized/active` 状态统一写成 `SESSION_AUTH_WINDOW_EXCEEDED`。该错误名只能表示真实 12h 到期，不能表示“尚未授权”“已拒绝”或“已关闭”。
7. `core/crates/er-client/src/host_stream.rs` 退出一次数据面循环后才清 `stats.session_id`。必须验证主控关闭、本机关闭和拒绝是否都能在 deadline 内触发该路径，不能依赖 90 秒 idle timeout。

## 4. 目标状态与唯一真相

### 4.1 建连

1. `requested`：主控只轮询/订阅会话状态；不得调用 relay ticket、握手、解码或显示“已连接”。
2. `authorized`：主控只触发一次可取消的 attach；并发轮询不得重复 attach。
3. `active`：双方 ready 且数据面建立后进入；计时、画面、输入和控制条只认真实 active/数据面事件。
4. attach 失败必须保留完整错误 envelope/trace_id，停止进行中状态，并提供合法重试；不得把原始 JSON 和内部 URL 直接铺在画面中央。

### 4.2 拒绝

1. 被控端点击拒绝或 30 秒超时后，服务端权威状态进入 `closed`，reason 分别为 `user_rejected` / `consent_timeout`（最终枚举以现有 spec/实现的统一值为准，不新增散落字符串）。
2. 主控在 2 秒内停止等待，停止/不启动 attach，展示“对方拒绝了本次连接”或“对方未响应，连接已取消”，并可返回设备页重新发起。
3. 拒绝是一次成功完成的本机决策。服务端写状态失败才算请求失败；不得出现“服务端已 closed，但 FFI/UI 仍认为拒绝没送达”的分裂结果。

### 4.3 任一端断开

1. 主控点击断开：先调用 GW-25 `POST /v1/sessions/close`，确认 `state=closed`；再停止本地数据面、释放 texture/buffer/task，最后离开页面。失败时留在页面并展示可复制 `trace_id`，不得谎报“已断开”。禁止另造 `/v1/connect/close` 等不存在的别名。
2. 被控端点击断开：按钮进入 pending；FFI 调用必须返回结构化成功/失败。确认服务端 `closed` 后清本地 session stats，继续保留被控服务等待下一次请求。失败时控制条保留并显示错误，不得乐观消失或反复出现。
3. 对端关闭：另一端通过关闭事件或 1 秒状态轮询兜底，在 2 秒内停止采集、编码、输入和心跳，撤销旧票据，清控制条/会话页并展示关闭原因。
4. GW-25 必须幂等。重复关闭不会复活会话、重复建立数据面或遗留旧 `session_id`。

### 4.4 授权窗口错误

1. 客户端在 `requested/closed` 状态不得调用 GW-11，这是本问题的首要修复和回归断言。
2. 服务端只有在授权确实存在且 `auth_expire_at < now` 时才能返回 `SESSION_AUTH_WINDOW_EXCEEDED`。
3. 对 `requested/closed` 的防御性返回不得伪装成“12 小时已到期”。如果现有错误集无法精确表达，先记录为单独 spec blocker，不在本卡偷改错误语义。

## 5. 能力链与改动边界

```text
主控 SessionPage
  -> GatewayClient status/close
  -> SessionVideo / er-ffi ctrl session
  -> er-client controller state/data plane
  -> gateway + session state machine
  -> close notifier / relay ticket revocation
  -> host_stream
  -> HostService status
  -> 被控端 LocalControlBar
  -> 两端日志、trace_id、测试
```

允许修改的生产文件仅限实际需要的下列集合；开工前在任务证据中记录最终白名单：

- `client/lib/api.dart`
- `client/lib/models.dart`
- `client/lib/pages/session_page.dart`
- `client/lib/pages/device_list_page.dart`
- `client/lib/session_video_io.dart`
- `client/lib/host_service_io.dart`
- `core/crates/er-client/src/agent.rs`
- `core/crates/er-client/src/ctrl_session.rs`
- `core/crates/er-client/src/host_stream.rs`
- `core/crates/er-ffi/src/host.rs`
- `core/crates/er-ffi/src/session.rs`
- `server/internal/session/session.go`
- `server/internal/gateway/handler.go`
- 对应的 Go/Rust/Dart 测试文件

禁止修改管理端、PRD、contract、error code 生成物、旧任务完成记录和无关版本发布文件。发现必须越界时先暂停并补充证据，不得顺手修改。

## 6. 必须先红后绿的验收测试

测试名可按现有语言命名规则微调，但一条验收断言必须有唯一可搜索测试名。

### 6.1 Flutter / Dart

- `TestSessionPageDoesNotAttachBeforeAuthorized`
- `TestSessionPageAttachesOnceAfterAuthorized`
- `TestRejectedSessionShowsPeerRejectedAndStopsPolling`
- `TestConsentTimeoutShowsCancelledAndStopsPolling`
- `TestControllerDisconnectClosesServerBeforeLeaving`
- `TestControllerDisconnectFailureDoesNotClaimSuccess`
- `TestLocalDisconnectFailureKeepsControlBar`
- `TestLocalControlBarClearsAfterConfirmedClose`
- `TestClosedStateReasonMapping`

测试必须注入可观察的 status/attach/close adapter，断言调用顺序和次数，不能只查文案或控件存在。

### 6.2 Go

- `TestConsentRejectedPersistsClosedAndReturnsContractResult`
- `TestConsentTimeoutPersistsClosedReason`
- `TestRelayTicketNotAuthorizedIsNotAuthWindowExceeded`
- `TestRelayTicketActualAuthExpiryReturnsAuthWindowExceeded`
- `TestSessionCloseIsIdempotentAndNotifiesBothPeers`
- `TestClosedStatusCarriesReason`

### 6.3 Rust / FFI

- `reject_decision_closes_server_and_clears_pending`
- `controller_close_stops_data_plane_and_releases_registry`
- `host_close_clears_current_session_without_stopping_host_service`
- `peer_close_clears_host_stats_within_deadline`
- `close_failure_is_returned_to_dart`

### 6.4 跨进程 E2E

建立真实 gateway + rendezvous + relay + 两个独立客户端/核心进程，至少覆盖：

1. 请求 -> 被控拒绝 -> 主控 2 秒内显示拒绝并退出等待。
2. 请求 -> 接受 -> 双方 active -> 主控断开 -> 被控 2 秒内清控制条并停止 frames 增长。
3. 请求 -> 接受 -> 双方 active -> 被控断开 -> 主控 2 秒内收到对端断开并停止 input/frame。
4. requested 阶段 GW-11 调用次数为 0；authorized 后每次 attach 只签发当前 epoch 票据。
5. 旧票据、旧授权决策和旧输入在 close 后全部失效。

推荐测试名：`TestRemoteLifecycleRejectAndBidirectionalDisconnect`。不能以同进程 fake、手工截图或只测 HTTP 状态代替。

## 7. 实施顺序

1. 保存当前 `git status --short`，明确哪些是用户/其它任务的未提交改动；禁止清理、reset 或覆盖。
2. 在 `docs/impl/evidence/ER-905/diagnosis.md` 写调用时序图，记录四个现象各自的实际调用、状态、reason、trace_id 和根因。
3. 写 6.1-6.3 的失败测试，确认每个测试修复前确实失败；不得先改实现再补绿测试。
4. 先修权威状态语义：拒绝、超时、close 的状态和 reason；不改变接口字段。
5. 再修主控生命周期：authorized 前不 attach；close 成功后才离开；closed reason 映射成明确终态。
6. 再修被控生命周期：close 结果可观察；失败不清 UI；服务端关闭后及时清 host stats 且 host service 回到等待下一会话。
7. 删除受影响链路中的固定 `sleep` 协调或为其另立阻塞卡。不得新增 `sleep`、扩大 timeout 或轮询掩盖竞态。
8. 跑跨进程 E2E，保存双方结构化日志和状态时间线；日志脱敏，不保存 token/device secret/画面。
9. 跑受影响技术栈全量测试和严格 gate。

## 8. 明确禁止的伪修复

- 把 `AuthWindow` 从 12h 改更长，或遇到 300006 自动重试/忽略。
- 在 `SessionPage.initState` 延迟几秒再 attach。
- 主控/被控点击断开时只清本地 widget/FFI 状态，不关闭服务端会话。
- 被控按钮无论 FFI 成败都隐藏控制条。
- 把 `closed` 当 `idle`，让 UI 继续等待。
- 拒绝后只发 signal toast，不写权威 session 状态。
- 通过增加固定 sleep、增大测试 timeout、重跑 flaky test 取得绿色结果。
- 修改 contract/error spec、删断言、skip E2E 或使用同进程 mock 证明跨端传播。

## 9. 闸门与完成定义

定向命令至少包括：

```bash
cd server && go test -race ./internal/session ./internal/gateway -run 'Consent|RelayTicket|SessionClose|ClosedStatus' -v
cd core && cargo test -p er-client -p er-ffi remote_lifecycle -- --nocapture
cd client && flutter test test/session_lifecycle_test.dart test/local_control_bar_test.dart test/consent_controller_test.dart
bash scripts/gate.sh
```

如果实际测试文件/过滤名不同，证据中列出真实命令，不能伪造上述命令已运行。

只有同时满足以下条件才能写“已完成”：

- 四个用户现象均由失败测试复现并转绿。
- 跨进程 E2E 三条拒绝/双向断开路径全绿，且 deadline 内状态收敛。
- requested 阶段没有 relay ticket 请求，300006 只由真实授权窗口到期触发。
- 断开后双方资源释放、旧票据/输入失效有测试证据。
- Flutter、Go、Rust 受影响全量检查与 `bash scripts/gate.sh` 全绿。
- `docs/impl/evidence/ER-905/` 包含诊断、命令输出、状态时间线、失败与修复后证据、已知限制和回滚方案。

## 10. 回滚

按客户端状态映射、服务端状态机/handler、Rust/FFI 生命周期三组分别提交或保留可独立还原的 diff。回滚不得恢复“只清 UI”的断开逻辑；若新实现出现严重回归，应禁用 `remote_control` capability 并回到设备页，不得 fail-open 或继续显示可操作的假会话。
