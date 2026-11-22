# ER-001a Rust 存量 lint 清零（候选卡）

## 来源与性质
ER-001 把 CI/gate 的 clippy 收紧为 `-D warnings` 后暴露的存量 lint（此前 CI 注释明确“暂不阻塞”）。属于未规划债务，按 AGENTS「旁路问题新建卡」处理；不并入业务卡。

## 目标与成熟度
`cargo clippy --workspace --all-targets -- -D warnings` 全绿，且不改变运行语义；Rust 栈从 lint 债务状态升为可严格闸门。

## 需求与字段映射
N/A（纯代码质量，不改接口/协议）。

## 当前证据
- `docs/impl/evidence/ER-001/clippy.log`（ER-001 首跑）。
- 具体项：
  - `er-capture/src/lib.rs:297` 非 Windows 目标下 `timeout_ms` 未使用。
  - `er-capture/src/privacy.rs:32/42/92`、`er-capture/src/lib.rs:155`：`chunks_exact(_mut)` 建议 `as_chunks(_mut)`。
  - `er-abr/examples/abr_curve.rs:12`：空格式串里塞字面量（print_literal）。
  - `er-codec/src/lib.rs:253`：`FakeEncoder.cfg` 只写不读。

## 只准改这些文件
- `core/crates/er-capture/src/lib.rs`
- `core/crates/er-capture/src/privacy.rs`
- `core/crates/er-capture/examples/privacy_check.rs`
- `core/crates/er-abr/examples/abr_curve.rs`
- `core/crates/er-codec/src/lib.rs`、`examples/nvenc_bench.rs`、`examples/pipeline_latency.rs`
- `core/crates/er-client/src/{agent,encoder,ctrl_session,link,lib}.rs`
- `core/crates/er-client/examples/{full_session_e2e,mac_controller,session_resume_e2e,weaknet_send,xmachine_controller,reconnect_e2e?}.rs`
- `core/crates/er-ffi/src/lib.rs`、`src/tests.rs`
- `core/crates/er-features/src/d2_power.rs`
- `core/crates/er-winapp/src/main.rs`
- `core/Cargo.lock`（工作区版本同步）
- `docs/impl/tasks/ER-001a-rust-lint.md`、`docs/impl/evidence/ER-001a/`

## 验收命令
```bash
export PATH="$(dirname "$(rustup which cargo)"):$PATH"
cd core && cargo fmt --all -- --check
cd core && cargo clippy --workspace --all-targets -- -D warnings
cd core && cargo test --workspace
```

## 设计与回滚
- 只做等价改写；`as_chunks` 的 remainder 必须显式忽略，像素遍历上限不变。
- 假编码器删掉未读字段；配置仍由 `new()` 校验。
- 回滚：逐文件 revert；不得把 clippy 退回非严格模式。

## 完成状态（2026-11-22）
- 完成：`cargo fmt --check`、`cargo clippy --workspace --all-targets -- -D warnings`、`cargo test --workspace` 全绿。
- 共清理：chunks_exact/as_chunks、unused import/variable/mut、needless_return、print_literal、dead_code、zombie_processes、manual_div_ceil、useless_conversion、too_many_arguments（显式 allow + 理由）。
- 证据：`docs/impl/evidence/ER-001a/README.md`、`docs/impl/evidence/ER-001/clippy-final.log`。
