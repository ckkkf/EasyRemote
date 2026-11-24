# ER-905 交付汇总

## 四个用户现象的闭环证据

| # | 现象 | 根因 | 修复 | 证据 |
|---|---|---|---|---|
| 1 | 进入会话即 403 `SESSION_AUTH_WINDOW_EXCEEDED` | `SessionPage.initState` 无条件 attach；服务端把「未授权」错写成「窗口到期」 | 客户端 authorized 前不 attach（`_startVideoOnce` 幂等） | `TestSessionPage*`、E2E[A] 无 ticket |
| 2 | 主控离开后被控端仍显示被控 | 主控只退 UI，不调 GW-25 | 所有退出入口（返回/取消/live 返回/系统返回）统一 `_disconnectAndLeave` → GW-25 | `TestClose*`、`TestBackToDevices*`、E2E[A] |
| 3 | 被控端断开会复活 | 忽略 close 结果 + 乐观清 UI + 轮询写回 | `HostClosePolicy` 结果驱动 + 墓碑去重（有界 32） | `TestLocal*`、`TestClosedTombstonesAreBounded` |
| 4 | 拒绝后主控一直等 | 服务端把拒绝当错误响应；客户端无 `closed` 分支 | 拒绝=完成态（200 + ConsentResult）；客户端 `closed→ended` + reason 分类 | `TestConsentRejected*`、`TestClosedStateReasonMapping`、E2E[C] |

## 跨进程 E2E（同一次运行，三个路径全绿）

```
$ OUT=/tmp/er905-e2e-dsh bash scripts/mac/e2e_lifecycle.sh
E2E[A] device=312188656
E2E[A]_PASS converged=1s          # 主控断开 → 被控端 ≤2s 收敛
E2E[B] device=987704278
E2E[B]_PASS PROBE_PEER_CLOSED reason=local_disconnect   # 被控断开 → 主控 ≤2s 观察
E2E[C] device=613851586
E2E[C]_PASS rejected              # 拒绝 → 主控 closed/user_rejected，且未 attach
E2E_ALL_PASS
```

进程构成：独立 stack + 独立 relay（随机端口）+ 独立 mac_host 进程 + 独立 dart probe 进程；
临时 `$HOME`、trap 清理（只杀自己 PID）、readiness 用 `curl -fsS`、日志 allowlist 脱敏。

## 定向测试

```
$ cd server && go test ./internal/session ./internal/gateway -run 'Consent|ClosedStatus|SessionClose|Lifecycle' -v
TestConsentRejectedPersistsClosedAndReturnsContractResult  PASS
TestConsentRejectedReturnsCompletedClosedState             PASS
TestRemoteLifecycleRejectAndBidirectionalDisconnect        PASS

$ cd core && cargo test -p er-client --lib
31 passed
peer_closed_state_predicate_matches_close_states           PASS

$ cd client && flutter test test/session_close_coordinator_test.dart test/session_lifecycle_test.dart \
    test/session_exit_routes_test.dart test/host_close_policy_test.dart \
    test/local_control_bar_busy_test.dart test/host_close_worker_test.dart test/close_path_contract_test.dart
All tests passed!
```

## Gate

`26-gate-final.txt`：20/20 步，`EXIT=0`，`GATE: PASS`。

## 已知限制（保留 beta）

1. **ER-905-PROTO-01**：`host_stream`/`mac_host` 各两处固定等待（1200ms/2500ms）用于等 UDP 中继注册；
   去掉必须给 relay 增加注册 ACK（protocol.md 变更）。见 `spike-relay-registration.md`。
2. **ER-905-SPEC-01**：GW-11 无法表达「未授权/已拒绝」，只能用 `SESSION_AUTH_WINDOW_EXCEEDED`；
   客户端已避免在 requested 阶段申请票据，服务端精确码待批准。
3. UI heartbeat 用「同步 worker 在 isolate 内不阻塞 ticker」的单元测试证明，未做真机 perf 采样。

## 回滚

- 客户端：`session_close_coordinator.dart`、`host_close_policy.dart` 为独立新增文件，删引用即可回到旧行为（但会恢复现象 2/3）。
- 服务端：`session.go` 的 Consent 分支改回 `return errcode.ConnConsentRejected` 会恢复现象 4，**不建议**。
- 若新实现出现严重回归：禁用 `remote_control` capability 并回到设备页，不得 fail-open。

## 最终审查修正（round 9，详见 30-final-review-fixes.md）

1. `session.go` 两处授权过期分支改为**先写 reason 再 ClassifyClose**；新增
   `TestAuthWindowExpirySetsCloseKindFromNewReason`（断言 CloseKind=timeout，顺序写反即红）。
2. `local_disconnect`（被控端本地断开的真实 reason，E2E 路径 B 实测值）映射到 `peerClosed`；
   主控显示「对方已结束远程控制」。遗留 `ER-905-PROTO-02`：reason 缺 actor 前缀。
3. `host_service_io.dart` 注释与 tear-off 注入实现对齐。
4. `e2e-run.stdout.txt` 已用同一次 A/B/C 运行覆盖。
5. `allowControl` 预写/回滚属被控开关功能，**不是本卡必需**：不回滚，但从 ER-905 交付范围排除，
   越界风险登记（与 `_hostClosing` 共用 State，后续改动需回归断开用例）。

复验：Go 定向 ok / Flutter 21 passed / `E2E_ALL_PASS` / `GATE: PASS`（`29-gate-round9.txt`）。

## round 10 补充：local_disconnect 的 CloseKind

之前只给 `local_disconnect` 加了 UI 映射（→ peerClosed），漏了服务端 `CloseKind`：
它会被 `contains("disconnect")` 判成 `peer_offline`（异常），与「用户主动断开」语义矛盾。
已显式映射为 `CloseUserHangup`，并在 `TestClassifyCloseCoversEr905Reasons` 中断言其不计入异常。
服务端统计口径与主控 UI 语义的差异已在注释中写明（观察点不同）。

复验：`go test ./internal/session ./internal/gateway` ok；`GATE: PASS`（`31-gate-closekind-local.txt`）。

## round 14：gate 全绿（以 39-gate-round14.txt 为准，38 作废）

- `attach_error_ui_test.dart` 重写为纯 SessionVideo 生命周期测试（去掉 material 导入与伪 UI 断言）；
  页面级断言保留在 `session_page_attach_ui_test.dart`（pump/tap/Completer 4 条全绿）。
- 契约生成物与 formatter 的冲突已从根上解决：生成器先 `dart format` 再输出，
  `--check` 两侧都格式化后比较 → `✓ 三端契约一致（73 类型 / 70 接口）`。
- 复验：dart format 0 changed / flutter analyze 0 error-warning（138 info 存量）/
  flutter test --concurrency=1 → 121 passed / E2E_ALL_PASS / `GATE: PASS`。
