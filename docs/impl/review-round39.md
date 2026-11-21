# 第三十九轮：三条断言同时 PASS（零拷贝 + 链路 + 输入精度）

> 日期：2026-11-20 ｜ 主控 agent 亲自执行

## 一、结果

```
CODEC: init kind=Nvenc 2880x1620
CODEC: prime_frame=true
CODEC: backend=gpu-zero-copy
CODEC: backend_final=gpu-zero-copy
WIN: frames_sent=8 bytes_total=66129 ms=753 dims=2880x1620
WIN: cursor_baseline=Some((189, 142)) cursor_baseline_recheck=Some((189, 142)) stable=true
WIN: injected_moves=[(400, 300), (363, 321), (400, 300)]
WIN: cursor_after=Some((400, 300))
WIN: RESULT frames_received=5 frames_sent=8 input_events=3 moved_to_target=true perturbation_stopped=true
WIN: VERDICT PASS
```

**输入落点 (400,300) 精确命中，零偏差。**

## 二、根因：DPI 感知的初始化时机

之前的偏移（(417,336)、(216,292)、(407,254)）不是随机，也不是"远端遥控软件抢光标"。

真正原因：`SetProcessDpiAwarenessContext` 在**首次注入时才调用**，此时进程可能已经创建过窗口/HWND，调用失败 → 进程停留在 DPI 虚拟化状态：

| 调用 | 在虚拟化状态下返回 |
|---|---|
| `GetSystemMetrics` | 物理分辨率 2880×1620 |
| `SetCursorPos` / `GetCursorPos` | 按**逻辑**坐标 1646×926 解释 |

两套坐标系混用 → 落点必然偏移，且偏移量取决于窗口被创建的顺序，看起来像随机。

修复（`er-input::windows`）：

```rust
pub fn init_process_dpi() -> bool { ... }

impl WindowsInjector {
    pub fn new(live: bool) -> Self {
        let dpi_ready = init_process_dpi();   // 构造即初始化，早于任何 HWND
        Self { live, dpi_ready }
    }
}
```

并且注入时若拿不到 DPI 感知，**直接返回 `InjectError::Unsupported`**，而不是给出一个偏几十像素的坐标——错误的坐标比明确的错误更难查。

## 三、之前的三个"排除项"其实都指向同一件事

| 我曾怀疑 | 实际 |
|---|---|
| jiggle 移动光标干扰 | 干扰存在，但即使停掉仍偏移 → 不是根因 |
| GameViewer 抢光标 | 停服务后仍偏移 → 不是根因 |
| 显示器断开（WinDisc/MousePresent=False） | **这是我在 session 0 里测的**，session 1 实际是 `\\.\DISPLAY1 1646x926, MousePresent=True` |

第三条是我犯的同一类错误第二次：**在错误的会话里测量，然后把结论套到正确会话上**。
第一次是"LogonUI 存在 = 锁屏"，这次是"session 0 没显示器 = 机器没显示器"。

## 四、验证汇总

| 断言 | 状态 | 证据 |
|---|---|---|
| `backend=gpu-zero-copy` | ✅ PASS | 目标端日志 |
| 链路收发帧 | ✅ PASS | 收 5 / 发 8，2880×1620 |
| 输入精确命中 ±2px | ✅ **PASS（0 偏差）** | cursor_after=(400,300) |
| ACCESS_LOST 自愈 | ⚠️ INCONCLUSIVE | 触发条件仍未构造 |

## 五、回归

| 端 | 结果 |
|---|---|
| Rust `er-input` | 18 passed |
| Rust 全量 | 157 passed（上一轮基线） |
| 真机跨机会话 | PASS |

## 六、下一步

1. ABR 闭环实测：lossyproxy 造 0/5/15% 丢包，验证码率/分辨率真降档（代码已接 `AbrController::on_feedback`，缺对照数据）
2. ACCESS_LOST：需要 UAC 安全桌面 / 外接屏热插拔 / RDP 重连才能触发
