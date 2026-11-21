# 第三十四轮：零拷贝接进产品路径 + DXGI ACCESS_LOST 自愈/锁屏检测

> 日期：2026-11-20 ｜ 机器：Mac(macOS) + WIN-VERIFY-PC(Windows 11, RTX 3060 Laptop)
> 证据：`docs/impl/evidence/windows-2026-11-20-gpu/`
> 约束：未启用任何代理/VPN；未放宽任何断言。
> 分工：`er-capture`/`er-codec::nvenc` 归 agent-3（本轮只调用其 API + 在其同意下改 dxgi.rs），
> `er-client`/`er-ffi`/`client/lib/native.dart` 归我。

---

## 一、要点：这一轮把「仅 bench」变成「产品路径」

上一轮（round31）零拷贝只在 `dxgi_nvenc_bench.rs` 里跑过，会话主路径仍然是
`cap.grab()`（CPU 回读）→ `codec.encode(bgra)`（CUDA 上传）。本轮把这个洞补上：

| 入口 | round31 状态 | 本轮状态 |
|---|---|---|
| 基准 `dxgi_nvenc_bench` | 走零拷贝 | 不变 |
| 会话（`er-client` 的 `send_screen_frames`） | CPU 回读 + CUDA 上传 | **GPU 零拷贝优先，失败自动回退 CPU** |
| FFI（`er-ffi`） | `grab` + `encode` 两步，中间过 Dart 内存 | **新增 `er_grab_and_encode` 一步到位**，同样 GPU 优先 |

新增/改动：

- `core/crates/er-client/src/gpu_policy.rs`（平台无关，**6 条单测**）
  `GpuPath` 状态机：`Uninitialized → Registered → Failed(reason) →（reset）→ Uninitialized`；
  `GpuFallback` 五种原因（NoHardwareEncoder / DimensionMismatch{expected,actual} / RegisterFailed /
  EncodeFailed / NotAvailable），`is_permanent()` 区分「重试没用」和「可以重试」；
  `take_log()` 保证同一原因**只打一次**日志。
- `core/crates/er-client/src/gpu_glue.rs`（Windows）：调 agent-3 的
  `NvencEncoder::new_d3d11` + `register_d3d11_input` + `encode_d3d11_registered`，
  配合 `DxgiCapture::device_raw` / `ensure_encoder_input` / `copy_held_to_encoder_input` / `grab_gpu`。
- `core/crates/er-client/src/encoder.rs`：`HybridEncoder` = 策略 + GPU 胶水 + CPU 编码器，
  对外只有 `capture_and_encode(cap)` 和 `backend()`。
- `agent.rs`：发送循环改用 `HybridEncoder`，日志从 `CODEC: kind=...` 变成
  `CODEC: backend=cpu|gpu-zero-copy kind=Nvenc 2880x1620`，收尾再打一行 `backend_final`。
- `er-ffi`：`Session` 持 `HybridEncoder`；新增
  `er_grab_and_encode(sess, buf, cap)` 与 `er_encoder_backend(sess)`；
  `client/lib/native.dart` 同步加 `grabAndEncode()` / `encoderBackend()`。

回退条件（三者任一即回退，且**不会 panic**）：

1. 本机没有硬件编码器（`er_codec::hardware_encoder_available()` 为假 / `new_d3d11` 失败）
2. 采集纹理尺寸 ≠ `EncodeConfig`（改分辨率、跨屏拖动、DPI 变化）
3. 注册/编码失败（设备被独占、纹理失效）

---

## 二、DXGI 自愈与锁屏检测（第 2 步）

改的都是 `er-capture/src/dxgi.rs`（agent-3 已确认落盘、可改）：

1. **ACCESS_LOST 自愈**：`grab_gpu` 拆成「自愈包装 + `acquire_once`」。
   撞到 `DXGI_ERROR_ACCESS_LOST`（切分辨率/换显示器/UAC/RDP 重连/驱动重置）时
   `rebuild_duplication()` **一次**并重试一次；再失败才返回 `CaptureError::Lost`。
   - 严格按 agent-3 的约束：**只重建 `IDXGIOutputDuplication`，绝不重建 device/context**
     （NVENC 会话与已注册纹理都绑在那块 device 上），重建前先 `release()` 丢 held；
   - desc 尺寸若变化，顺带重建 staging 并把 `encoder_input` 置空（尺寸相关资源全部作废）。
2. **锁屏检测**：`OpenInputDesktop` + `GetUserObjectInformationW(UOI_NAME)`，
   名字不是 `Default` 即视为锁屏；结果 **1s TTL 缓存**（不每帧查）。
   锁屏时 `grab_gpu` 直接返回 `Ok(None)`（契约内），**不再向上吐错误**。
   查不出来（非交互会话/无权限）时保守判为「未锁屏」，避免把正常错误吞掉。
3. **休眠与锁屏分开**（agent-3 提醒的坑）：连续 5 次 `WAIT_TIMEOUT` 才
   `wake_display()`（`ES_DISPLAY_REQUIRED`）一次，然后继续正常返回 `Ok(None)`。
   **产品路径不调用 `nudge_desktop()`**——那会真的移动用户光标，基准脚本才需要它。
4. 新增 windows feature `Win32_System_StationsAndDesktops`。

---

## 三、验证

### 3.1 不需要 GPU 的单测（本轮核心，macOS 上就跑）

```
$ cargo test -p er-client --lib
test gpu_policy::tests::no_hardware_encoder_falls_back_without_retry ... ok
test gpu_policy::tests::dimension_mismatch_falls_back_and_reports_both_sizes ... ok
test gpu_policy::tests::register_failure_retries_then_falls_back ... ok
test gpu_policy::tests::matching_dims_keep_gpu_path ... ok
test gpu_policy::tests::state_machine_transitions ... ok
test gpu_policy::tests::no_fallback_reason_panics ... ok
test result: ok. 6 passed; 0 failed
```

覆盖：三种回退情形（无硬件 / 尺寸不符 / 注册失败）都必须回到 CPU 且不 panic；
状态机 `Uninitialized→Registered→Failed→reset→Uninitialized→Registered`；
同一原因只打一次日志；尺寸一致时不得误判回退。

### 3.2 全量回归

| 平台 | 命令 | 结果 |
|---|---|---|
| Windows | `cargo build -p er-capture -p er-codec -p er-client -p er-ffi` | `BUILD_EXIT=0` |
| Windows | `cargo test --workspace` | **141 passed / 0 failed**，`TEST_EXIT=0` |
| macOS | `cargo test --workspace` | **140 passed / 0 failed** |
| macOS | `flutter test` | 18 passed |

Windows 141 = 上一轮基线 135 + 本轮新增 6 条 `gpu_policy` 单测；
macOS 140 = 141 − 2（`nvenc::tests` 是 Windows-only）。**没有放宽任何旧断言，也没有删测试。**

### 3.3 Windows 上 ABI 真的导出了（唯一能在锁屏下做的运行级检查）

`dumpbin /exports target\debug\er_ffi.dll`（证据 `ffi-dll-exports.txt`）：

```
er_encoder_backend = er_encoder_backend
er_encoder_kind    = er_encoder_kind
er_grab_and_encode = er_grab_and_encode
er_hardware_encoder_available = er_hardware_encoder_available
```

说明：这证明四个 ABI 真在 dll 里，**但不证明运行行为正确**（Windows 上还没调用过它们）。
顺带记录一个真实翻车：我第一次加这两个函数的那次编辑因为脚本里一个重复标识符解析失败，
整段没执行，直到 dumpbin 发现导出缺失才补上——所以「导出表检查」这一步不是形式主义。

### 3.4 明确没有验证到的（不粉饰）

| 项 | 状态 |
|---|---|
| `backend=gpu-zero-copy` 的真机会话日志 | ❌ **没拿到**。被控机当前锁屏（LogonUI 在跑），DXGI 在锁屏下不出帧，GPU 路径无法被驱动。等解锁后补 |
| ACCESS_LOST 自愈的真实触发 | ❌ 未实测。要触发得在采集过程中切分辨率/拔显示器/UAC，本轮只有代码与编译验证 |
| 锁屏检测的误判率 | ❌ 未实测。只在锁屏状态下确认了「会返回 Ok(None) 而不是刷错误」（间接：本轮构建与测试期间没出现错误风暴），但没有对照「解锁时不应误判」的真机日志 |
| 零拷贝的收益复核 | ❌ 本轮没有重跑 bench。round31 的数字（encode 8.30→7.04ms、capture 6.47→8.47ms）仍是上一轮的，且那台机现在锁屏 |
| GPU 路径下的端到端时延 | ❌ 未测（同上） |

一句话：**代码已经接进产品路径并编译/单测通过，但「真机跑起来」这一步没做，不能算已验证。**

---

## 四、复现

```bash
# 单测（任何平台，不需要 GPU）
cargo test -p er-client --lib gpu_policy

# 全量
cargo test --workspace

# Windows 侧
scp /tmp/er-core.tgz win:C:/EasyRemote-core.tgz
ssh win 'cmd /c "C:\EasyRemote\mp.cmd cmd /c \"cd /d C:\EasyRemote\core && cargo test --workspace\""'

# 解锁后应看到（目前还没拿到）：
#   CODEC: backend=gpu-zero-copy kind=Nvenc 2880x1620
#   CODEC: backend_final=gpu-zero-copy
```

---

## 五、step3 待执行（解锁后按顺序跑，脚本已就位）

被控机当前**锁屏**（`LogonUI.exe` 在跑），所以 step3 的四个断言一条都还没真机跑过。
四个脚本已经写好并**验证过「锁屏时 SKIP」这条路径**（见
`docs/impl/evidence/windows-2026-11-20-gpu/step3-preflight-skip.log`：四个脚本全部输出
`SKIP: desktop is locked ... no PASS/FAIL produced`、退出码 4）。

| # | 脚本 | 断言 | 当前状态 |
|---|---|---|---|
| 1 | `scripts/windows/run_lockstate.cmd` | 解锁状态下日志出现 `is_locked=false` + `RESULT: PASS` | ⏳ 待解锁 |
| 2 | `scripts/windows/run_access_lost.cmd` | `rebuilds_after > rebuilds_before` 且 `device_ptr_unchanged=true` 且 `nvenc_session_reused=true` | ⏳ 待解锁 |
| 3 | `scripts/windows/run_bench_compare.cmd [iters]` | `METRIC dxgi+NVENC(copy)` 与 `METRIC dxgi+NVENC(zero-copy)` 两条都在，且 `CAPTURE_BUDGET_40MS` 行存在 | ⏳ 待解锁 |
| 4 | `ssh win run_xmachine_gpu.cmd` + `bash scripts/mac/step3_xmachine.sh` | 被控端日志出现 `CODEC: backend=gpu-zero-copy` 与 `backend_final=gpu-zero-copy`，且 `moved_to_target=true` | ⏳ 待解锁 |

执行顺序与产出：

1. `run_lockstate.cmd` → `C:\EasyRemote\step3logs\lockstate.log`
2. `run_access_lost.cmd` → `access_lost.log`（顺带确认重建后还能出帧）
3. `run_bench_compare.cmd 20` → `bench_compare.log` + 对照表
4. `run_xmachine_gpu.cmd` + Mac 侧 `step3_xmachine.sh` → `xmachine_gpu.log`（被控端）、
   `step3-xmachine-controller-macos.log`（主控端）

全部跑完后，把四个日志 scp 到
`docs/impl/evidence/windows-2026-11-20-gpu/`，并在本文件把上表的 ⏳ 换成实测结果。

### 已知风险（跑之前先看）

- **显示器模式切换可能是空操作**：`run_access_lost.cmd` 优先切「同分辨率不同刷新率」；
  某些笔记本面板/虚拟显示器不接受，会返回 `RESULT: INCONCLUSIVE`（不是 FAIL）。
  真遇到这种情况要换触发方式（例如插拔外接屏、或 Win+P 切换投影模式）。
- **跨机脚本用的是固定手机号参数**：服务端有短信频控（`AUTH_SMS_RATE_LIMITED`），
  Mac 侧脚本会自动生成新号并传进来；手动跑 `run_xmachine_gpu.cmd` 时记得换号。
- **首个 IDR 可能被 GPU 路径吃掉**：如果 `backend=gpu-zero-copy` 出现在日志里但
  `frames_received=0`，先看是不是握手阶段就回退了（日志会有一行 `[er] 零拷贝路径不可用`）。

---

## 六、step3 实测进展（2026-11-20 20:30 更新）——含一条**推翻我上一节结论**的更正

### 6.1 更正：不是锁屏，是「静止桌面」

我上一节按 `LogonUI.exe` 进程判断「锁屏」是**错的**：LogonUI 在解锁状态下也可能驻留。
在交互会话里跑 `dxgi_step3.exe lockstate` 得到权威结论：

```
DXGI_LOCKSTATE adapter="NVIDIA GeForce RTX 3060 Laptop GPU" size=2880x1620 format=87 mode=Some((2880, 1620, 120))
DXGI_LOCKSTATE is_locked=false
RESULT: PASS (is_locked=false)
```

所以四个脚本里基于 LogonUI 的前置**已删除**，改为「payload 自己判定」：
`dxgi_step3 lockstate/selfheal` 在真锁屏时返回 `RESULT: SKIP` + 退出码 3，脚本映射为 SKIP。
`run_xmachine_gpu.cmd` 另外加了 `:lockcheck`（在交互会话里跑 lockstate，`is_locked=false` 才继续）。

### 6.2 静止桌面为什么会「没帧」

agent-3 的实测（`docs/impl/evidence/windows-2026-11-20-dxgi/`）：静态桌面下
`AcquireNextFrame` 恒 `0x887A0027`(WAIT_TIMEOUT)，`wake_display()` 单独调用**无效**；
补一次 **dx=dy=0 的 SendInput** 后立刻出帧，且光标不动。

已落地：
- `DxgiCapture::grab_gpu`：连续 5 次超时 → `wake_display()` + `nudge_desktop_zero()`
- `gpu_glue`：`grab_gpu(0)` 拿不到 → nudge 后重试一次 `grab_gpu(30)`；仍无帧则本帧走 CPU，
  并且**只打一次**日志 `[er] GPU 路径：补零位移输入后仍无新帧（静止桌面？），本帧走 CPU`
  （之前的问题正是「backend=cpu 但日志里没有任何原因」，这种最难查）

### 6.3 ACCESS_LOST 自愈：实现了两级，但**本机触发方式不可用**

用 `ChangeDisplaySettingsW` 切刷新率（2880x1620@120 → @60）来触发，实测链路：

| 阶段 | 观察 | 结论 |
|---|---|---|
| 切模式后第一次 `AcquireNextFrame` | `hresult=0x887A0001` = **DXGI_ERROR_INVALID_CALL**，不是 ACCESS_LOST | 只映射 ACCESS_LOST 的自愈根本不触发（第一版就是这个问题） |
| 把 INVALID_CALL 也纳入自愈 | `rebuilds_after=1`，但之后 30 次全部 `Lost` | 只换 duplication 不够 |
| 升级到「整条重建 device」 | `rebuilds_after=2`、`device_rebuilds=1`、device 指针变化、NVENC 会话重建 | 重建路径本身работает |
| 重建后继续取帧 | 仍是 `Lost`；**但同一时刻新起一个进程立刻能正常采集**（lockstate PASS，2880x1620@120） | 长生命周期进程在模式切换后**恢复不了** |

因此：
- 代码层面已实现「duplication → device 整条重建 → 重新枚举 adapter」三级尝试；
- 但 `ChangeDisplaySettings` 这个触发手段在本机**无法验证自愈成功**，
  `run_access_lost.cmd` 会如实返回 `INCONCLUSIVE`，不会假报 PASS；
- 真正要验证 ACCESS_LOST 自愈，需要换触发方式（UAC 安全桌面、外接显示器热插拔、RDP 重连），
  或者把「模式切换」按「整个 ScreenCapture 重建」处理（这属于产品层策略，不是 duplication 自愈）。

### 6.4 跨机会话：链路 PASS，但**零拷贝没吃到**（重要）

20:21 跑了一次完整的 `run_xmachine_gpu.cmd` + Mac 主控（证据
`step3-xmachine-windows.log`）：

```
WIN: frames_received=5 resolution=1920x1080 first_h264_bytes=65149
WIN: capture_display=2880x1620
CODEC: backend=cpu kind=Nvenc 2880x1620        <-- 注意：不是 gpu-zero-copy
CODEC: backend_final=cpu
WIN: frames_sent=8 bytes_total=67560 ms=3378 dims=2880x1620
WIN: cursor_before=Some((1642, 961))
WIN: cursor_after=Some((399, 299))  moved_to_target=true
WIN: VERDICT PASS
```

结论要分开说：
- **跨机全链路 PASS**：采集 → 编码 → 中继 → 解码 → 注入 全部真实跑通，注入后光标到位；
- **零拷贝没启用**：`backend=cpu`，编码器虽然是 Nvenc（硬件），但数据是
  「CPU 回读 → CUDA 上传」，不是「DXGI 纹理 → NVENC 直接编码」。
  按 `run_xmachine_gpu.cmd` 的断言（要求 `backend=gpu-zero-copy`）这条**算 FAIL**，不是 PASS。

下一步定位（已加日志）：先看是不是「静止桌面拿不到帧 → 本帧走 CPU」，
还是「尺寸不符/注册失败」。日志里现在会明确写出一行原因。

### 6.5 step3 四个断言的当前状态

| # | 断言 | 状态 |
|---|---|---|
| 1 | `is_locked=false`（解锁对照） | ✅ **已实测通过**（`step3-lockstate.log`） |
| 2 | ACCESS_LOST 自愈 | ⚠️ **触发方式不可用/自愈不完整**（见 6.3），脚本返回 INCONCLUSIVE |
| 3 | copy vs zero-copy 对照表 | ⏳ 未跑（可随时跑，不依赖 Mac） |
| 4 | 跨机 `backend=gpu-zero-copy` | ❌ **未通过**：链路 PASS 但后端是 cpu（见 6.4） |

---

## 七、step3 修复与真机结果（2026-11-20 20:45）

### 7.1 根因修复：Ok(None) 的语义（lead 定位，已确认）

`er-client/src/encoder.rs` 原来把 GPU 返回的 `Ok(None)`（静止桌面无新帧）当成「没走 GPU」，
于是落 CPU 编码并把 backend 标成 cpu，且**这条路径不打日志**。已改成单一决策点：

| GPU 返回 | 行为 |
|---|---|
| `Ok(Some)` | backend=gpu-zero-copy |
| `Ok(None)` | 直接返回 `Ok(None)`：不编码、不落 CPU、不改 backend（`GpuPath::on_no_frame()` 显式空操作） |
| `Err` | 停用 GPU 路径 + 打一次回退日志 + 本帧交 CPU |

新增 5 条单测（`encoder::tests`），钉住三条语义：NoFrame 不改 backend、NoFrame 不调用 CPU（计数 mock 为 0）、
Err 才回退（CPU 调用 1 次）。`cargo test -p er-client --lib` → **11 passed / 0 failed**。

另补：`ScreenCapture::prime_frame()`（零位移 nudge）+ 发送循环开头调用，保证新订阅者能拿到首帧。

### 7.2 真机结果：零拷贝**真的启用**了

```
WIN: capture_display=2880x1620
CODEC: init kind=Nvenc 2880x1620
CODEC: prime_frame=true
CODEC: backend=gpu-zero-copy          <-- 这次真的吃到了
CODEC: backend_final=gpu-zero-copy
WIN: frames_sent=8 bytes_total=40551 ms=954 dims=2880x1620
```

对比修复前后同一路径：`frames_sent=8` 从 **3378ms → 954~980ms**（8 帧），
与 `dxgi+NVENC(zero-copy)` 的 bench 数字量级一致。证据：
`step3-xmachine-gpu-zero-copy.log`。

### 7.3 但这一跑 step3 仍判 FAIL —— 卡在输入精度，不是零拷贝

```
WIN: screen=2880x1620
WIN: cursor_after=Some((404, 298))
WIN: moved_to_target=false (target=Some((400, 300)) actual=Some((404, 298)))
WIN: VERDICT FAIL
```

- 目标是 (400,300)，实际落在 (404,298)：**偏差 (+4, -2) px**，超出 ±2px 容差；
- 同一个断言在上一轮（screen=1646x926）是精确命中 (399,299)；
- 变化点是本机现在按 2880x1620 物理分辨率上报（之前 1646x926 是 DPI 缩放后的逻辑值）。

判断：`SendInput` 的 0..65535 绝对坐标 + `SetProcessDpiAwarenessContext` 的时机/生效范围
在这个 DPI 下产生了系统性偏移，**不是随机误差**（两次落点一致）。

**我没有放宽 ±2px 容差**。这条按未解决记录，下一轮要么：
1. 在创建注入器之前（进程启动早期）设置 per-monitor-v2 DPI 感知，再读一次 screen size 校验；
2. 或改用 `SetCursorPos` + 只对按键/点击用 SendInput（鼠标移动用 SetCursorPos 是精确像素）。

### 7.4 step3 四断言最终状态

| # | 断言 | 状态 |
|---|---|---|
| 1 | 解锁对照 `is_locked=false` | ✅ PASS（早前实测） |
| 2 | ACCESS_LOST 自愈 | ⚠️ INCONCLUSIVE（触发方式不可用；lead 已同意不改断言，产品层退化为「重建整个 ScreenCapture」，**这是退化方案**） |
| 3 | copy vs zero-copy 对照表 | ⏳ 未跑 |
| 4 | 跨机 `backend=gpu-zero-copy` | 🟡 **后端断言通过**（backend/backend_final 都是 gpu-zero-copy），但同一条脚本的输入断言 FAIL（4px 偏差） |
