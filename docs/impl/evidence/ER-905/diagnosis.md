# ER-905 诊断（diagnosis）

日期：2026-11-23 · 工作区 HEAD：267a635（`00-head.txt`）
基线 git status 快照：`00-git-status-baseline.txt`（256 条，均为其它任务/用户未提交改动，本卡不动）

## 0. 结论摘要

四个用户现象不是四个 UI bug，而是**同一条权威闭环断在三处**：

1. `requested` 阶段就 attach → GW-11 被拒 → 服务端把「未授权」错写成 `SESSION_AUTH_WINDOW_EXCEEDED`。
2. 拒绝决策被服务端当成**错误响应**（HTTP 4xx），而 GW-07 契约要求 `ConsentResult` 完成态 → 客户端拿不到 closed。
3. 客户端 `closed` 没有映射成终态（落到 `SessionState.idle`），且 `reason` 只塞进 message 字符串 → UI 永远等。
4. 两端「断开」都只清本地（主控不调 GW-25；被控忽略 FFI 返回值）→ 服务端会话与对端状态不收敛。

## 1. 现象 → 实际调用链 → 根因

### 现象 1：进入会话即 `POST /v1/relay/ticket` 403 `SESSION_AUTH_WINDOW_EXCEEDED`

```text
SessionPage.initState()
  └─ _startVideo()                      ← 无条件执行，不看 state
       └─ SessionVideo.attach(gateway, relay, token, deviceId, deviceToken, sessionId)
            └─ FFI er_ctrl_attach
                 └─ ctrl_session::attach → ControllerAgent::begin(session_id)
                      └─ gateway POST /v1/relay/ticket
                           └─ handler.go:779
                                if sess.State != authorized && != active {
                                    errcode.SessionAuthWindowExceeded  ← 错码（此处未授权，非 12h 到期）
                                }
```

证据（代码行）：
- `client/lib/pages/session_page.dart`：`initState` 内直接 `_startVideo()`。
- `server/internal/gateway/handler.go:779-784`：非 authorized/active 统一抛 `SESSION_AUTH_WINDOW_EXCEEDED`。
- `docs/spec/error-codes.md:109`：该码语义只表示真实授权窗口到期。

根因：**调用时序错了**（requested 就申请票据），而服务端的兜底错误码**误导**了排查方向。

### 现象 2：主控离开后，被控端仍显示「正在被远程控制」

```text
SessionPage 点击断开
  └─ _leaveWith('已断开连接')        ← 只退 UI
       └─ （没有 POST /v1/sessions/close，GW-25 未调用）
被控端 host_stream 继续推流 / stats.session_id 仍在
```

根因：**主控断开没有关闭领域会话**；被控端因此永远等不到关闭事件，只能靠 90s idle 或心跳超时兜底。

### 现象 3：被控端点「断开远程控制」后控制条仍存在或复现

```text
device_list_page._disconnectRemote()
  └─ HostService.closeSession(...)   ← 返回值 bool 被忽略
  └─ 立即 setState(idle)             ← 乐观更新
  └─ 2s 后 FFI 轮询 status()          ← 旧 session_id 又写回 UI → 控制条复现
```

根因：**close 结果不可观察 + 乐观清 UI + 轮询把旧值写回**。

### 现象 4：被控端拒绝后，主控仍在等待

```text
被控端点拒绝
  └─ POST /v1/connect/consent {accepted:false}
       └─ session.transition(CommandConsent, !accepted)
            next.State = closed; next.Reason = user_rejected
            return &next, true, errcode.ConnConsentRejected   ← 状态已 closed，却返回 error
       └─ handler.go:456-460  if err != nil { WriteErr(...) }  ← 写成 4xx 错误体
主控轮询 GET /v1/connect/status
  └─ api.dart:627  switch(state) { 'closed' 没有分支 }  → _ => SessionState.idle
       └─ UI 认为「还没授权」，继续等待
```

根因（三处叠加）：
1. 服务端把「拒绝」这一**成功的本机决策**写成错误响应（违反 GW-07 `response: ConsentResult`，契约 errors 也不含 `CONN_CONSENT_REJECTED`）。
2. 客户端 `sessionStatus` 没有 `closed` 分支，落 `idle`。
3. `reason` 未分类，UI 无法区分 rejected / consent_timeout / peer close。

## 2. 契约核对（不改 spec）

| 端点 | 契约要求 | 现状 |
|---|---|---|
| GW-07 `/v1/connect/consent` | `response: ConsentResult`；errors 仅 `CONN_DEVICE_NOT_FOUND, ADMIN_FORBIDDEN` | 拒绝时返回 4xx `CONN_CONSENT_REJECTED` ❌ |
| GW-08 `/v1/connect/status` | `SessionStateResult`（state/reason） | 服务端有 reason，客户端丢分类 ❌ |
| GW-11 `/v1/relay/ticket` | errors 含 `SESSION_AUTH_WINDOW_EXCEEDED`（仅真实窗口到期） | 未授权也用它 ❌ |
| GW-25 `/v1/sessions/close` | `session_id/state/reason` | 客户端从不调用 ❌ |

## 3. Spec blocker（不在本卡偷改）

`GW-11` 的错误集没有「会话尚未授权 / 已被拒绝 / 已关闭」的精确码。
按任务卡 4.4.3：本卡**不新增/不改** `error-codes.md`；先把客户端侧「requested 不申请票据」修掉（本卡首要断言），服务端精确码作为独立 blocker：

- blocker id：`ER-905-SPEC-01`
- 需要的表达：`SESSION_NOT_AUTHORIZED`（requested）、`SESSION_CLOSED`（已拒绝/已关闭）
- 阻塞范围：仅 GW-11 的防御性错误码；不影响拒绝/断开/时序主线

## 4. 修复顺序（与任务卡第 7 节一致）

1. 服务端：拒绝/超时写成**完成态**（closed + reason），Consent 不返回 error；GW-25 幂等 + 通知双方。
2. 客户端：`closed` 映射终态 + reason 分类；requested 不 attach；authorized 只 attach 一次。
3. 断开：主控先 GW-25 再离开；被控用 close 结果驱动 UI，失败保留控制条。
4. Rust：host_stream 在 close 事件/状态轮询下 deadline 内清 session_id，不依赖 idle。
5. 跨进程 E2E + gate。