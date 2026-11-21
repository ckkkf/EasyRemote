# Windows 真机证据（2026-11-20）

机器：WIN-VERIFY-PC（Windows 11 专业工作站版 26200，RTX 3060 Laptop，驱动 610.88）
接入：ssh win = <frp-host>:<port>（frp）→ 本机 22
约束：全程**未启用任何代理/VPN**；实测 warp-cli status = Disconnected，http_proxy/https_proxy 均为空。

| 文件 | 来源 | 说明 |
|---|---|---|
| cargo-clean-build-windows.log | Windows C:\EasyRemote\clean-build.log | **cargo clean 后**的完整构建：removed 6335 files / 4.1GiB，重编 188 个 crate，Finished in 35.98s，BUILD_EXIT=0 |
| cargo-build-windows.log | Windows C:\EasyRemote\build.log | 首次全量 cargo build --workspace（BUILD_EXIT=0，32.80s） |
| cargo-test-windows.log | Windows C:\EasyRemote\test-after-clean.log | clean 构建之后的 cargo test --workspace：135 passed / 0 failed，TEST_EXIT=0 |
| nvenc-bench-interactive.log | Windows C:\EasyRemote\bench.log（schtasks /IT 交互会话） | NVENC vs OpenH264 同帧对比 + 解码尺寸校验 |
| xmachine-target-windows.log | Windows C:\EasyRemote\target.log（schtasks /IT） | 跨机被控端原始输出：收到的帧数、发出的帧数、注入后光标坐标 |
| xmachine-controller-macos.log | Mac xmachine_controller | 跨机主控端原始输出：会话建立、握手、收发帧数、输入事件数 |
| stack.log / relay.log / udptun.log | Mac 服务端进程 stdout | 真实 gateway+rendezvous（stack）/ 真实 UDP 中继（relay）/ UDP-over-TCP 隧道（udptun） |

复现命令见 docs/impl/review-round30.md 的「四、复现」，Windows 侧脚本模板见 scripts/windows/。
