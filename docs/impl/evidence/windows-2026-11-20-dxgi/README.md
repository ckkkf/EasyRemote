# Windows DXGI Desktop Duplication + NVENC 零拷贝证据（2026-11-20）

机器：WIN-VERIFY-PC（Windows 11 专业工作站版 build 26200，RTX 3060 Laptop，驱动 610.88）
工具链：cargo 1.100.0 / rustc 1.100.0（见 env-windows.txt）
分辨率：2880x1620（DISPLAY1，DXGI output#0，format=87 = B8G8R8A8_UNORM）
约束：全程未启用任何代理/VPN；基准必须在**交互会话**跑（schtasks /IT）。

## 文件索引

| 文件 | 来源 | 说明 |
|---|---|---|
| dxgi-bench-run1.log | Windows C:\EasyRemote\dxgi_bench.log（19:32 运行） | 四路径 + 两条 DirectX 单独编码路径的完整原始输出（VERDICT: PASS） |
| dxgi-bench-run2.log | 同上（19:29 运行） | 同参数复跑，用来看抖动 |
| dxgi-probe.log | Windows C:\EasyRemote\dxgi_probe.log | 早期诊断：只有 SetCursorPos 时 AcquireNextFrame 恒 WAIT_TIMEOUT |
| desktop-state.log | Windows C:\EasyRemote\desktop_state.log | 交互会话状态：input desktop=Default、shell 存在、LogonUI 在跑 |
| er-capture-real-capture.log | Windows cargo test -p er-capture -- --nocapture | Session 0 里 DXGI 打不开 -> 回退 xcap，测试按设计 skip |
| cargo-test-windows.log | Windows cargo test --workspace | 135 passed / 0 failed / exit 0 |
| cargo-test-macos.log | Mac cargo test --workspace | 134 passed / 0 failed（macOS 侧无回归） |
| env-windows.txt | Windows | cargo/rustc/OS 版本 |

原始日志里 `DXGI_BENCH_START ...` 行由 cmd.exe 用 GBK 写 %DATE%/%TIME%，中文显示为乱码；
exe 之后的输出是 UTF-8，正常。

## 关键原始输出（dxgi-bench-run1.log）

~~~
=== DXGI 拓扑 ===
adapter#0 name="NVIDIA GeForce RTX 3060 Laptop GPU" vendor=0x10de luid=00000000:000148ed
    output#0 attached=true rect=(0,0)-(2880,1620) device="\\\\.\\DISPLAY1"
adapter#1 name="AMD Radeon(TM) Graphics" vendor=0x1002 luid=00000000:00016425
    (no DXGI outputs)
adapter#2 name="NVIDIA GeForce RTX 3060 Laptop GPU" vendor=0x10de luid=00000000:000207e2
    (no DXGI outputs)
adapter#3 name="Microsoft Basic Render Driver" vendor=0x1414 luid=00000000:000163af
    (no DXGI outputs)

=== Desktop Duplication 取帧探测 ===
wake_display = true
acquire #0: ERR hresult=0x887a0027 msg=超时值已过，资源还不可用。 (0x887A0027)
nudge(sendinput) #0: ok=true
acquire #1: OK texture=true last_present=0 accum=0 last_mouse=266689857877 ptr_shape_B=9216 ptr_pos=(1514,903) visible=true
nudge(sendinput) #1: ok=true
acquire #2: OK texture=true last_present=266691589123 accum=12 ...
dxgi_live = true
~~~

~~~
=== 汇总（20 帧/路径，2880x1620）===
路径                             capture_ms    encode_ms       aux_ms    e2e_fps           decode    idr_B
xcap+NVENC(copy)                   646.12         8.24         0.00       1.53     2880x1620 OK    13511
dxgi+NVENC(copy)                     6.74         8.24         0.00      63.53     2880x1620 OK    15253
dxgi+NVENC(zero-copy)                9.56         7.11         0.00      59.60     2880x1620 OK    15253
dxgi+NVENC(direct)                   8.99         6.51         0.00      64.32     2880x1620 OK    15253
nvenc(directx, 常驻注册)                  n/a         6.76         0.00        n/a     2880x1620 OK    13511
nvenc(directx, 每帧注册)                  n/a         6.37         0.00        n/a     2880x1620 OK    13511

CAPTURE_BUDGET_40MS: xcap_baseline=646.12ms dxgi_zero_copy=9.56ms
RESULT: paths=6 all_dims_ok=true
VERDICT: PASS
~~~

## 三次运行（同参数）对照

| 路径 | capture ms/frame（19:27 / 19:29 / 19:32） | encode ms/frame | 端到端 fps |
|---|---|---|---|
| xcap + NVENC(copy) | 647.81 / 646.94 / 646.12 | 8.24 / 8.25 / 8.24 | 1.52 / 1.52 / 1.53 |
| dxgi + NVENC(copy) | 7.31 / 6.47 / 6.74 | 8.24 / 8.30 / 8.24 | 61.58 / 64.43 / 63.53 |
| dxgi + NVENC(zero-copy) | 8.46 / 8.47 / 9.56 | 7.18 / 7.04 / 7.11 | 63.47 / 63.99 / 59.60 |
| dxgi + NVENC(direct) | 9.90 / 9.79 / 8.99 | 6.41 / 6.47 / 6.51 | 61.09 / 61.30 / 64.32 |

六条路径的首帧 IDR 全部解回 2880x1620（解码尺寸校验 OK）；三/四路的 IDR 大小同为 15253/15228/15264 量级，
说明它们编码的是同一块屏幕、同一时刻的内容。

## 复现

~~~
# Mac 侧同步源码
cd core && tar czf - crates/er-capture crates/er-codec | ssh win "cd C:\\EasyRemote\\core && tar xzf -"

# Windows 侧构建（必须带 MSVC 环境）
C:\\EasyRemote\\mp.cmd cmd /c "cd /d C:\\EasyRemote\\core && cargo build --example dxgi_nvenc_bench -p er-codec"

# 交互会话里跑（Session 0 里 DXGI 一定拿不到桌面）
schtasks /create /tn ERDxgi /tr "C:\\EasyRemote\\run_dxgi_bench.cmd 20" /sc once /st 00:00 /ru Administrator /it /f
schtasks /run /tn ERDxgi
type C:\\EasyRemote\\dxgi_bench.log
~~~

## 已知限制（别当成已解决）

1. **桌面休眠/静止时 AcquireNextFrame 恒返回 DXGI_ERROR_WAIT_TIMEOUT**。
   本轮一开始就撞上：只有 xcap（GDI BitBlt）能出帧，DXGI 三次 acquire 全超时（见 dxgi-probe.log）。
   根因是显示器休眠 / DWM 停止提交帧；基准里用 SetThreadExecutionState(ES_DISPLAY_REQUIRED|ES_CONTINUOUS)
   + SendInput 鼠标微动解锁（见日志里 acquire #1 起出帧）。产品侧必须显式处理，否则远程桌面静置后采集会停住。
2. 所有数字都是 **debug 构建**；release 未测。
3. 每条路径只跑 20 帧，未做分钟级抖动/内存增长观察。
4. 只验证到「采集 -> 编码」这一段；没有把新采集后端接进 er-client/er-ffi 的跨机全链路重跑
   （那条链路目前仍是 xcap 采集，见 review-round30 的证据）。

---

## 追加：20:10 复盘修正（覆盖前面「显示器休眠」的说法）

前面写的「显示器休眠」是错的，也不是锁屏。用 `dxgi-probe-nonudge.log` / `dxgi-probe-zeromove.log` 对比：

- 只调 `wake_display()`（ES_DISPLAY_REQUIRED）+ 不制造输入：`is_locked=false`、`wake_display=true`，
  但 `acquire #1..#5` 全 `0x887A0027`，`grab_gpu_after = None`。
- 每轮补一次 `dx=dy=0` 的 `SendInput`（**不移动光标**，`ptr_pos` 保持不变）：`acquire #1..#5` 全部 OK，
  `grab_gpu_after = Some(2880x1620)`。

真正原因：**桌面静止 → DXGI 没有 present → 按设计无帧**。所以：

| 口径 | capture ms/frame | e2e fps | 文件 |
|---|---|---|---|
| 内容在变化（60Hz 有 present） | 6.5 ~ 9.6 | 59.6 ~ 64.3 | dxgi-bench-run1.log / -run2.log |
| nudge 后帧已在队列（下界） | 0.48 ~ 0.49 | 133 ~ 146 | dxgi-bench-run3-pending-frame.log |
| 桌面完全静止 | 无帧（Ok(None)） | n/a | dxgi-probe-nonudge.log |

对外结论只引用第一行（6.5~9.6ms）。新增文件：dxgi-probe-nonudge.log、dxgi-probe-zeromove.log、
dxgi-bench-run3-pending-frame.log、cargo-test-windows-141.log、cargo-test-macos-140.log
（agent-1 合并 step1~step3 后 Windows **141 passed**、macOS **140 passed**）。

`DxgiCapture::nudge_desktop_zero()` 是为此加的：零位移输入唤醒 present 且不动光标。
