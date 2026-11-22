# ER-001a 证据：Rust 存量 lint 清零

- 状态: 完成（`cargo clippy --workspace --all-targets -- -D warnings` exit 0）

## 修复类别

| 类别 | 位置 |
|---|---|
| chunks_exact(_mut) → as_chunks(_mut) | er-capture/lib.rs、privacy.rs、er-client/ctrl_session.rs、er-ffi/lib.rs、er-ffi/tests.rs |
| unused import/variable/mut | er-codec 示例、er-client 示例、er-ffi、er-winapp、er-client/link.rs |
| dead_code | er-codec FakeEncoder.cfg、er-client classify_gpu_result/GpuDecision/CountingEncoder、agent.gateway_base |
| needless_return | er-features/d2_power.rs |
| zombie_processes | reconnect_e2e、session_resume_e2e（kill 后 wait） |
| manual_div_ceil / manual_is_multiple_of / useless_conversion / print_literal | er-winapp、er-ffi、er-abr example |
| too_many_arguments | er-winapp::net_thread（显式 allow + 理由） |

## 验证

```
$ cargo fmt --all -- --check        # pass
$ cargo clippy --workspace --all-targets -- -D warnings   # exit 0
$ cargo test --workspace            # 全部 test result: ok
```

日志：`clippy.log`、`cargo-test.log`。
残留在输出里的 `er-ffi` 未使用依赖 `er-features` 是 cargo manifest 提示（非 clippy error），未改依赖图。
