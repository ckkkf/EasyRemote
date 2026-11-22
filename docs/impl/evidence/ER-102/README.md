# ER-102 证据：host_stream 只认本地决策

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 变更

| 文件 | 目的 |
|---|---|
| core/crates/er-client/src/host_consent.rs | ConsentBroker：pending/decide/wait；超时=false；stop 唤醒 |
| host_stream.rs | 删除 target.consent 自动调用；请求 → 等决策 → 接受才 consent，拒绝/超时调 reject |
| er-ffi/src/host.rs | er_host_pending_consent / er_host_decide_consent |
| client/lib/host_service_io.dart | start 后 500ms 轮询 pending → ConsentController；决策回写 FFI；释放新增字符串指针 |
| client/lib/pages/device_list_page.dart | 监听 controller，展示/关闭 ConsentDialog |
| capabilities.yaml | remote_host → experimental（发布仍关闭） |

## 2. 不变量

- 等待方只有两个结果：本机 decision 或超时 false；无自动 true。
- 决策按 session 匹配；其他 id 返回 false 且不改状态。
- 拒绝/超时显式 reject，避免僵尸 authorized。
- stop 清空 broker 并唤醒等待线程。
- Flutter 轮询只把 pending 喂给 controller；决策只来自本机 UI。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 无决策不会自动同意 | TestHostNeverAutoConsents（Rust） | PASS |
| 只有匹配 session 可接受 | local_accept_only_for_matching_session | PASS |
| 拒绝正确传播 | local_reject_is_propagated | PASS |
| controller 接受/拒绝/超时/跨 session | ER-101 的 4 条 | PASS |
| 弹窗 UI | ER-101 的 2 条 widget | PASS |
| 能力门发布禁用 | TestIncompleteCapabilityIsDisabled | PASS |

## 4. 命令与输出

    $ cd core && cargo test -p er-client host_consent
    3 passed
    $ cargo clippy --workspace --all-targets -- -D warnings
    exit 0
    $ cd client && flutter test test/consent_controller_test.dart test/consent_dialog_test.dart test/capability_gate_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 一次性 serve_host（E2E/CLI 用）仍会自动 consent；它不在 Flutter 生产入口，但不得在发布构建暴露。ER-900 改 E2E 为显式决策，ER-202 补关闭传播与票据撤销。
- 会话中常驻控制条（ER-700）尚未实现；当前弹窗只覆盖授权瞬间。
- controller_label 由 session 前缀生成，尚未展示账号/设备脱敏信息；ER-900/后台可补。

## 6. 回滚

- 回滚 broker 会恢复自动同意，属于安全红线退化，禁止。
- serve_host 的自动同意只能以“限制到测试构建”方式回滚，不能进入生产。
