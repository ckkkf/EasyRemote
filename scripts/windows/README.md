# Windows 侧脚本（真机验证用）

这些脚本在 Windows 机器上运行（验证机：WIN-VERIFY-PC，RTX 3060 Laptop）。
**脚本一律 CRLF + 纯 ASCII**：cmd.exe 不认 LF，中文在 cp936 控制台会变乱码。

| 脚本 | 作用 | 需要交互会话 |
|---|---|---|
| `mp.cmd` | 带上 MSVC 环境执行任意命令（cargo 需要，否则找不到 link.exe） | 否 |
| `run_target.cmd` | 跨机被控端（历史脚本，键盘/采集演示） | 是 |
| `run_bench.cmd` | NVENC vs OpenH264 对比 | 是 |
| `run_lockstate.cmd` | **step3**：解锁状态下 `is_locked()` 必须为 false（锁屏检测误判的对照） | 是 |
| `run_access_lost.cmd` | **step3**：改分辨率触发 `ACCESS_LOST`，验证自愈且 device/NVENC 会话未重建 | 是 |
| `run_bench_compare.cmd` | **step3**：`dxgi_nvenc_bench` 的 copy vs zero-copy 对照表（配 `bench_compare.ps1`） | 是 |
| `run_xmachine_gpu.cmd` | **step3**：跨机会话的被控端，断言日志里出现 `backend=gpu-zero-copy` | 是 |
| `run_win2mac.cmd` | **step4**：跨机会话的**主控端**（Windows 控 macOS）。只发输入/收画面，不注入本机 | 否（可在 SSH 会话跑） |
| `bench_compare.ps1` | `run_bench_compare.cmd` 的 METRIC 解析器 | — |

## 前置

1. Rust + MSVC BuildTools（本机 rustc 1.100.0 / MSVC 14.44.35207）。
2. 依赖走 rsproxy 且强制直连（由 `core/.cargo/config.toml` 固化）。
3. **桌面必须已解锁**：DXGI Desktop Duplication 在锁屏下不出帧，
   四个 step3 脚本检测到 `LogonUI.exe` 会直接 `SKIP` 并以退出码 4 退出，
   **不会**产出 PASS/FAIL（避免误导性结论）。
4. 二进制先构建好：`cargo build -p er-capture -p er-codec -p er-client -p er-ffi --examples`。

## step3 用法

```powershell
# 1) 锁屏检测对照（解锁状态 → PASS，日志里 is_locked=false）
ssh win "cmd /c C:\EasyRemote\run_lockstate.cmd"

# 2) ACCESS_LOST 自愈（日志出现 rebuilds_before=0 rebuilds_after=1、
#    device_ptr_unchanged=true、nvenc_session_reused=true → PASS）
ssh win "cmd /c C:\EasyRemote\run_access_lost.cmd"

# 3) copy vs zero-copy 对照（可传迭代次数，默认 20）
ssh win "cmd /c C:\EasyRemote\run_bench_compare.cmd 20"

# 4) 跨机会话（两段式，见下）
ssh win "cmd /c C:\EasyRemote\run_xmachine_gpu.cmd 13700007101 13700007102"
```

日志统一落在 `C:\EasyRemote\step3logs\<name>.log`，完成后从 Mac 侧 scp 到
`docs/impl/evidence/windows-2026-11-20-gpu/`。

### 为什么跨机是两段式

主控是 **Mac 上的 Rust 二进制**（真采集 + 主控侧逻辑），而 Windows 机器没有 Mac 的
SSH 密钥，无法自己把主控拉起来。所以：

- Windows：`run_xmachine_gpu.cmd` 起被控端（schtasks /IT）→ 写出 device_code/captcha
  → 等 `C:\EasyRemote\gpu_session.done` 旗标 → 断言日志。
- Mac：`scripts/mac/step3_xmachine.sh [主控手机号]` 负责起 stack/relay/udptun、开
  `ssh -R` 隧道、读 hello、跑 `xmachine_controller`、最后 scp 旗标回去。

硬把两段拼成"一键"只会造出假数据，所以这里明确保持两段。

## step4 用法（反向：Windows 主控 + macOS 被控）

```powershell
# 建议由 Mac 侧一键编排（会起服务、开隧道、拉被控端、断言）：
#   bash scripts/mac/step4_win2mac.sh
# 手动单跑（Mac 侧已把 hello 放到 C:\EasyRemote\mac_hello.txt 之后）：
ssh win "cmd /c C:\EasyRemote\run_win2mac.cmd 13700008102"
```

与 step3 的关键差异：**Windows 是主控端**，只发输入、收画面，不往本机注入、不读本机光标，
所以不需要交互桌面（不用 schtasks /IT）。日志落在 `C:\EasyRemote\step4logs\win_controller.log`。

## 退出码约定（step3 四个脚本一致）

| 码 | 含义 |
|---|---|
| 0 | PASS（日志里有 `RESULT: PASS`） |
| 1 | FAIL / INCONCLUSIVE |
| 3 | SKIP（payload 自己判定跳过，例如没有 dxgi） |
| 4 | SKIP（前置不满足：锁屏 / 没有 NVIDIA 驱动） |
