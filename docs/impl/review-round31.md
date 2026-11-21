# 第三十一轮：采集从 xcap 换成 DXGI Desktop Duplication，并把纹理零拷贝喂给 NVENC

> 日期：2026-11-20 ｜ 目标：端到端主瓶颈（采集 600ms/帧）换掉，且采集到的 ID3D11Texture2D 不再回 CPU
> 机器：WIN-VERIFY-PC（Windows 11 专业工作站版 26200，RTX 3060 Laptop，驱动 610.88，2880x1620）
> 接入：ssh win（<frp-host>:<port>），基准走 schtasks /IT 交互会话
> 约束：**全程未启用任何代理/VPN**；未放宽任何断言
> 原始输出：docs/impl/evidence/windows-2026-11-20-dxgi/

---

## 一、结论先行

| 指标 | 换之前（xcap + NVENC/CUDA 上传） | 换之后（DXGI + NVENC/DirectX 零拷贝） | 变化 |
|---|---|---|---|
| 采集 capture ms/帧 | **646.12 ~ 647.81** | **8.46 ~ 9.56** | **约 70~76 倍** |
| 编码 encode ms/帧 | 8.24 | 7.04 ~ 7.18 | -1.1ms（省掉 cuMemcpy2D） |
| 采集+编码 ms/帧 | 655 | 15.5 ~ 16.7 | 约 40 倍 |
| 端到端 fps（采集+编码，无网络） | **1.52** | **59.6 ~ 63.5** | 达标 |
| 解码尺寸校验 | 2880x1620 | 2880x1620 | 全部一致 |

**40ms 预算：达标。** 采集 ~8.5ms + 编码 ~7.1ms = ~15.6ms，还剩约 24ms 给传输/解码/渲染。

「采集从约 600ms 降到多少」的准确回答：**2880x1620 一帧从 646~648ms 降到 8.5~9.6ms**（三次运行），
其中真正的 memcpy 成本已经不存在；剩下的 8~9ms 主要是 `AcquireNextFrame` 等 DWM 出帧的等待时间，
不是拷贝时间（同机对照：DXGI 读回 CPU 的 B 路径 6.5~7.3ms，零拷贝 C 路径 8.5~9.6ms）。

---

## 二、做了什么

### 2.1 er-capture：新增 Windows DXGI Desktop Duplication 后端

- 新增 `crates/er-capture/src/dxgi.rs`（`cfg(target_os="windows")`），基于 windows 0.60（与 xcap 已锁定的版本对齐，不新增传递依赖）：
  - `DxgiCapture::open_primary()`：枚举所有 DXGI 适配器/输出，优先「桌面坐标覆盖 (0,0)」的输出；
    对每个候选建 D3D11 设备 + `IDXGIOutput1::DuplicateOutput`，第一个成功的胜出（失败原因逐条保留）。
  - `grab_gpu(timeout)`：`AcquireNextFrame` 直接返回桌面 `ID3D11Texture2D*`，**不做任何 CPU 拷贝**；
    超时返回 `Ok(None)`（契约：桌面无变化返回 None），ACCESS_LOST 映射 `CaptureError::Lost`。
  - `grab_cpu(timeout)`：staging 纹理 `Map` 读回紧排 BGRA（给仍需要 CPU 像素的上层用）。
  - `ensure_encoder_input(w,h)` / `copy_held_to_encoder_input()`：一块常驻的 `D3D11_USAGE_DEFAULT` 输入纹理 +
    纯 GPU `CopyResource`，供 NVENC 常驻注册。
  - `device_raw()`：把 `ID3D11Device*` 交给 NVENC，保证「采集设备 == 编码设备」。
  - `upload_bgra_texture()`：把一帧 host BGRA 上传成 D3D11 纹理（用于在没有桌面可复制时单独验证编码器）。
  - `topology()` / `probe_report()` / `acquire_debug()` / `nudge_desktop()` / `wake_display()`：诊断与保活。
- `Capture` trait **未改**：`next_frame(&mut self, timeout_ms) -> Result<Option<RawFrame>, CaptureError>` 签名一字未动。
- `ScreenCapture` 内部改成 `enum Backend { Xcap(..), #[cfg(windows)] Dxgi(..) }`：
  Windows 上先试 DXGI，失败回退 xcap；`grab()` 在 DXGI 拿不到新帧时回退 xcap 保证一定出帧（不清空历史行为）。
  macOS/Linux 仍然原样走 xcap，逻辑零改动。`open_primary_xcap()` 保留，供对照基准强制走老路径。

### 2.2 er-codec/nvenc：新增 DIRECTX 输入路径（去掉 cuMemcpy2D）

- 抽出 `open_session(cfg, device_type, device)`：CUDA 路径 `NV_ENC_DEVICE_TYPE_CUDA`，新路径 `NV_ENC_DEVICE_TYPE_DIRECTX`（=0）。
- 新 API：
  - `NvencEncoder::new_d3d11(cfg, device: *mut c_void)`：会话直接开在调用方的 `ID3D11Device` 上。
  - `register_d3d11_input(tex)`：`nvEncRegisterResource(NV_ENC_INPUT_RESOURCE_TYPE_DIRECTX, pitch=0)` + `nvEncMapInputResource`，一次注册多帧复用。
  - `encode_d3d11_registered(ts)`：编码已注册纹理（零 host 拷贝）。
  - `encode_d3d11_texture(tex, ts)`：每帧 register/map/encode/unmap/unregister，直接吃外来纹理。
  - `submit(ts)`：三条路径共用的 `nvEncEncodePicture` + `nvEncLockBitstream`。
- 老路径（`NvencEncoder::new` + `encode_bgra` + `Encoder` trait）行为完全不变，`create_encoder()` 选择逻辑不变。

### 2.3 基准：crates/er-codec/examples/dxgi_nvenc_bench.rs

六条被测路径（同一台机、同一分辨率、同一编码配置 2880x1620@60 CBR 8Mbps gop60 bframes0，20 帧/路径）：

| 路径 | 采集 | 编码输入 |
|---|---|---|
| A `xcap+NVENC(copy)` | xcap（GDI 全帧 CPU 拷贝） | cuMemcpy2D 上传到 CUDA 设备指针 |
| B `dxgi+NVENC(copy)` | DXGI staging 读回 CPU | cuMemcpy2D 上传 |
| C `dxgi+NVENC(zero-copy)` | DXGI 纹理 + GPU `CopyResource` 到常驻纹理 | 常驻注册（无 cuMemcpy2D、无 host 拷贝） |
| D `dxgi+NVENC(direct)` | DXGI 桌面纹理直接注册 | 每帧 register，无 GPU 拷贝 |
| E `nvenc(directx, 常驻注册)` | 无（host 帧上传一次） | 常驻注册 |
| E2 `nvenc(directx, 每帧注册)` | 无 | 每帧 register |

每条路径都打印 capture ms/frame、encode ms/frame、端到端 fps，并把首帧 IDR 解回来校验尺寸（`H264Decoder`）。

---

## 三、原始数据（三次独立运行，debug 构建）

| 路径 | capture ms/frame | encode ms/frame | 端到端 fps | 解码 |
|---|---|---|---|---|
| xcap+NVENC(copy) | 647.81 / 646.94 / 646.12 | 8.24 / 8.25 / 8.24 | 1.52 / 1.52 / 1.53 | 2880x1620 OK |
| dxgi+NVENC(copy) | 7.31 / 6.47 / 6.74 | 8.24 / 8.30 / 8.24 | 61.58 / 64.43 / 63.53 | 2880x1620 OK |
| dxgi+NVENC(zero-copy) | 8.46 / 8.47 / 9.56 | 7.18 / 7.04 / 7.11 | 63.47 / 63.99 / 59.60 | 2880x1620 OK |
| dxgi+NVENC(direct) | 9.90 / 9.79 / 8.99 | 6.41 / 6.47 / 6.51 | 61.09 / 61.30 / 64.32 | 2880x1620 OK |
| nvenc(directx, 常驻注册) | n/a | 6.78 / 6.74 / 6.76 | n/a | 2880x1620 OK |
| nvenc(directx, 每帧注册) | n/a | 6.43 / 6.15 / 6.37 | n/a | 2880x1620 OK |

完整原始输出见 `docs/impl/evidence/windows-2026-11-20-dxgi/dxgi-bench-run1.log` / `-run2.log`，
其中都带 `RESULT: paths=6 all_dims_ok=true` 与 `VERDICT: PASS`。

---

## 四、40ms 预算逐层拆账（2880x1620 一帧）

| 层 | 本轮实测 | 说明 |
|---|---|---|
| DXGI 采集（acquire+提交） | 8.5 ~ 9.6ms | 主要是等 DWM 出帧；`CopyResource` 提交后由 GPU 异步完成，CPU 侧计时 0.00ms |
| NVENC 编码（DirectX 输入） | 7.0 ~ 7.2ms | debug 构建；同一块纹理 6.4~6.8ms（E/E2） |
| 采集+编码 | **15.5 ~ 16.7ms** | 余量 ~23ms |
| 传输 + 解码 + 渲染 | 未测 | 上一轮跨机链路只到「收到帧」，本轮没有再压 |

**是否达标：达标（采集+编码 15.5ms < 40ms）。** 没有「还差在哪一层」的缺口；
但如果要把「端到端」定义到屏幕到屏幕（含网络+解码+渲染），那一段本轮没有数据，不能算已验证。

---

## 五、踩到的真问题（不是代码 bug，但必须记录）

1. **桌面休眠时 Desktop Duplication 一帧都拿不到**。
   第一版 bench 里 A 路径正常（xcap 646ms），B/C/D 全部 `FAIL: no DXGI frame`；
   `dxgi_probe` 显示 `AcquireNextFrame` 三次全部 `0x887A0027 DXGI_ERROR_WAIT_TIMEOUT`，
   连鼠标微动（`SetCursorPos`）都无效。
   对照 `desktop_state.ps1`：`input_desktop_name=Default`、shell 窗口在、LogonUI 进程在；
   结论是**显示器已休眠 / DWM 停止提交帧**，不是会话锁死。
   修法：`SetThreadExecutionState(ES_CONTINUOUS|ES_DISPLAY_REQUIRED|ES_SYSTEM_REQUIRED)` + `SendInput` 鼠标位移
   （`SetCursorPos` 不算输入事件，唤不醒）。改完之后 acquire 立刻出帧，`dxgi_live = true`。
   → 产品侧必须内建这条保活，否则用户静置几分钟后采集会「假死」，这属于必须写进实现的坑。
2. **同一时刻只能持有一帧**。DXGI 要求 `ReleaseFrame` 后才能 `AcquireNextFrame`，
   所以 API 设计成「取帧 -> 用完 release」，`GpuFrame` 里的纹理指针只在本次持有期内有效。
3. **NVENC 的 DirectX 会话必须和采集用同一块 `ID3D11Device`**。
   本机拓扑恰好有利：`adapter#0 NVIDIA RTX 3060 Laptop` 上 output#0 覆盖 (0,0)-(2880,1620)，
   AMD 集显与另一块 NVIDIA 适配器都没有 DXGI 输出（见日志「DXGI 拓扑」段），所以一条设备能同时
   承担采集与编码。若换成 Optimus 由 iGPU 驱动的机器，零拷贝需要走共享纹理跨适配器，本轮未实现。

---

## 六、没做完 / 不能算已验证的（如实标注）

1. **release 构建未测**：上面所有数字都是 debug（`target/debug`）。release 下编码应该更快，但没数据。
2. **只有 20 帧/路径**：没做分钟级长时间抖动、显存增长、`DXGI_ERROR_ACCESS_LOST` 后的重连恢复。
   `Lost` 分支只映射了错误，未实现「重新 DuplicateOutput」的自愈。
3. **没有接进 er-client / er-ffi 的跨机全链路**：本轮验证到「采集 -> 编码」为止；
   跨机会话仍走 `ScreenCapture::open_primary()`（现在 Windows 上会优先 DXGI，但没有重跑跨机证据）。
4. **Session 0 下 DXGI 一定失败**（`er-capture-real-capture.log` 显示回退 xcap 后 skip），
   所以 Windows 侧任何采集验证都必须 `schtasks /IT`，这条约束没变。
5. **macOS 侧**只做了回归（`cargo test --workspace` 134 passed / 0 failed，见 cargo-test-macos.log），
   没有也无法跑 DXGI 路径；`ScreenCapture` 的 macOS 分支代码未改动。

---

## 七、回归

| 平台 | 命令 | 结果 |
|---|---|---|
| Windows | `cargo test --workspace` | **135 passed / 0 failed / exit 0**（与上一轮基线一致，见 cargo-test-windows.log） |
| macOS | `cargo test --workspace` | **134 passed / 0 failed**（见 cargo-test-macos.log） |

Windows 上 `-p er-capture` 单跑（`--nocapture`）可见 DXGI 在 Session 0 打不开 -> 回退 xcap -> 无显示器 skip，
断言没有放宽：`real_capture_produces_well_formed_frame` 仍然要求「非全零、尺寸自洽」，拿不到屏就 skip。

---

## 八、复现

~~~
# 1) Mac 侧同步（只同步我负责的两个 crate）
cd core && tar czf - crates/er-capture crates/er-codec | ssh win "cd C:\\EasyRemote\\core && tar xzf -"

# 2) Windows 侧构建
C:\\EasyRemote\\mp.cmd cmd /c "cd /d C:\\EasyRemote\\core && cargo build --example dxgi_nvenc_bench -p er-codec"

# 3) 交互会话里跑（关键：/IT）
schtasks /create /tn ERDxgi /tr "C:\\EasyRemote\\run_dxgi_bench.cmd 20" /sc once /st 00:00 /ru Administrator /it /f
schtasks /run /tn ERDxgi && type C:\\EasyRemote\\dxgi_bench.log

# 4) 诊断（拿不到帧时先跑这个）
schtasks /create /tn ERProbe /tr "C:\\EasyRemote\\run_dxgi_probe.cmd" /sc once /st 00:00 /ru Administrator /it /f
schtasks /run /tn ERProbe && type C:\\EasyRemote\\dxgi_probe.log
~~~

---

## 九、下一步（按收益排序）

1. 把 `dxgi+NVENC(zero-copy)` 接进 er-client 的被控端发送循环，重跑跨机会话，拿到「屏幕->屏幕」的端到端延迟。
2. 加 `DXGI_ERROR_ACCESS_LOST` 自愈（重建 Duplication）+ 显示器休眠保活（`wake_display` 已在库里）。
3. release 构建复测这六条路径，确认 40ms 预算的真实余量。
4. 若目标机是 iGPU 驱动桌面（无 NVIDIA 输出），补共享纹理跨适配器路径（`D3D11_RESOURCE_MISC_SHARED_NTHANDLE`）。

---

## 十、复盘修正（20:10，与 agent-1 联调后的第二次测量）

### 10.1 更正第五节第 1 条的根因

之前写的「显示器休眠」不准确，也不是「锁屏」。实测（`dxgi-probe-nonudge.log` / `dxgi-probe-zeromove.log`）：

| 场景 | is_locked | wake_display | acquire 结果 | grab_gpu_after |
|---|---|---|---|---|
| 只调 wake_display，不制造输入 | false | true | #0 拿到首帧，#1~#5 全 WAIT_TIMEOUT | **None** |
| 每轮补一次 dx=dy=0 的 SendInput | false | true | #1~#5 全部 OK（指针位置不变） | **Some(2880x1620)** |

结论：**桌面静止时 DXGI 按设计不产出帧**（没有 present 就没有帧），`is_locked=false` 证明会话没锁；
`wake_display` 单独无效；而 `dx=dy=0` 的 MOUSEEVENTF_MOVE **不移动光标**却能触发一次 present，
是产品侧唯一可用的「唤醒」手段（已落在 `DxgiCapture::nudge_desktop_zero()`）。

### 10.2 基准口径必须分开说（否则数字会误导）

| 口径 | capture ms/frame | e2e fps | 出处 |
|---|---|---|---|
| 桌面内容在变化（60Hz 有 present） | 6.5 ~ 9.6 | 59.6 ~ 64.3 | dxgi-bench-run1/run2.log |
| nudge 后帧已在队列里再 acquire（下界） | 0.48 ~ 0.49 | 133 ~ 146 | dxgi-bench-run3-pending-frame.log |
| 桌面完全静止 | 没有帧（Ok(None)） | 不适用 | dxgi-probe-nonudge.log |

第 4 节表里的 8.46/9.56ms 属于第一行（有真实内容变化），用它做结论；0.48ms 是「已有一帧待取」的下界，
不能当成「采集只要 0.5ms」卖。对 40ms 预算而言两种口径都达标，但对外只能引用第一行。

### 10.3 新基线（agent-1 的 step1/step2/step3 合并后）

| 平台 | 结果 | 文件 |
|---|---|---|
| Windows | **141 passed / 0 failed**（原 135 + 6 条 gpu_policy 单测） | cargo-test-windows-141.log |
| macOS | **140 passed / 0 failed** | cargo-test-macos-140.log |

agent-1 在 er-capture 侧追加了：ACCESS_LOST 重建 duplication（只换 duplication，不重建 device）、
`is_locked()`（OpenInputDesktop + UOI_NAME，1s TTL）、`rebuild_count()`、`display::switch_mode()`。
我的基准在这版代码上重跑仍然 PASS（run3），说明自愈与锁屏门控没有破坏零拷贝路径。

### 10.4 给产品路径的建议（未实现，只是结论）

1. 静止桌面返回 `Ok(None)` 是正确行为：上游重复上一帧即可，**不要**用阻塞 120ms 的 acquire 去等帧，
   否则一帧要 120ms（run3 之前的实测：capture 120~133ms、端到端只有 7fps）。
2. 若上层确实需要「立刻要一帧」（例如新观众加入后首帧），调用 `nudge_desktop_zero()` 再 acquire。
3. 把「静止」当失败（SKIP/exit 4）会让自动化永远拿不到证据；静止与锁屏要分开判定。

---

## 十一、ACCESS_LOST 的结论边界（按 lead 裁定记录）

- agent-1 的 dxgi_step3 自愈用例本轮 **INCONCLUSIVE**（没能稳定构造出 ACCESS_LOST 触发条件），**没有放宽断言**，
  因此这一条在本轮证据里的状态是「代码路径存在、触发未验证」，不能写成「自愈已验证」。
- 产品层允许**退化方案**：ACCESS_LOST 时重建整个 `ScreenCapture`（含 D3D11 device）。
  代价是 NVENC 会话与已注册纹理全部作废，必须重新 `new_d3d11` + `register_d3d11_input`；
  这条是降级路径，不是零成本自愈，文档与代码注释都要这么写。

## 十二、Windows 侧 er-client 副本是旧的（协作坑，已告知 agent-1）

复查时发现 `C:\EasyRemote\core\crates\er-client\src\gpu_policy.rs` 仍是旧版（没有 `on_no_frame`），
所以「Windows 141 passed」这个基线只有在把最新 er-client 同步过去并 `cargo clean -p er-client` 之后才能复现；
本轮我改用 `cargo test -p er-capture -p er-codec` 验证自己这部分：**er-capture 8 passed / er-codec 7 passed**，
确认新增的 `nudge_desktop_zero()` 没有破坏原有断言。

另：本地 er-client 当时 `cargo check -p er-client` 通过、`cargo test -p er-client` 11 passed，
gpu_policy.rs 里 `on_no_frame` 只有 1 处、没有 `GpuPolicy` 标识符 —— agent-1 报的「重复 on_no_frame + GpuPolicy::new()」
在磁盘上不存在（应是编辑器未保存 buffer 或 Windows 旧副本所致）。
