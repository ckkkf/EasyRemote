# 零拷贝接入产品路径 + DXGI 自愈证据（2026-11-20）

| 文件 | 说明 |
|---|---|
| gpu_build-windows.log | Windows: `cargo build -p er-capture -p er-codec -p er-client -p er-ffi` → BUILD_EXIT=0 |
| cargo-test-windows-141.log | Windows: `cargo test --workspace` → **141 passed / 0 failed**（135 基线 + 6 条新增 gpu_policy 单测），TEST_EXIT=0 |
| cargo-test-macos-140.log | macOS: `cargo test --workspace` → **140 passed / 0 failed**（macOS 无 nvenc 的 2 条） |

> 说明：本目录**没有** `backend=gpu-zero-copy` 的会话运行日志。
> 原因是被控机当前锁屏（LogonUI 在跑），DXGI Desktop Duplication 在锁屏下不出帧，
> 真实 GPU 路径无法被驱动。解锁后需要补一条会话日志才算真机验证。
