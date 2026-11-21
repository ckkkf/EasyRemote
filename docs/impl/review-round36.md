# 第三十六轮：零拷贝「静默回退」根因与修复

> 日期：2026-11-20

## 一、根因（我定位，agent-1 已按此重写）

上一轮跨机会话日志出现：

```
CODEC: backend=cpu kind=Nvenc 2880x1620
CODEC: backend_final=cpu
```

链路全通、编码器是硬件，但零拷贝没启用，而且**没有任何回退原因**。

根因在 `HybridEncoder::capture_and_encode` 的三分支里：

```rust
match self.gpu.try_encode(...) {
    Ok(Some(bytes)) => { backend = "gpu-zero-copy"; ... }
    Ok(None) => { /* 落到 CPU 路径 */ }          // ← 这里
    Err(reason) => { 停用 + 打日志 + 回退 }
}
```

`Ok(None)` 的语义是「本轮桌面没有新帧」（静止桌面时 `AcquireNextFrame` 超时），
但代码把它当成「GPU 路径没走成」，于是**落 CPU 编码并把 backend 标成 cpu**，
而且这条分支不打日志。

**所以「静默回退」其实是语义混淆：静止桌面被误判成回退。**

## 二、修复

判定集中到一个函数 `encode_frame_with_gpu`，四态分明：

| GPU 结果 | 行为 |
|---|---|
| `Ok(Some(bytes))` | backend=gpu-zero-copy，用 GPU 结果 |
| `Ok(None)` | **返回 Ok(None)，不动 backend、不调 CPU** |
| `Err(e)` | 停用 + 打一次日志 + 本帧交 CPU |
| `None` | 无 GPU 路径，直接 CPU |

`Ok(None)` 分支调用 `policy.on_no_frame()`（空操作，只为把语义钉死），
上游看到 `Ok(None)` 就重复上一帧。

## 三、测试

agent-1 写的 `CountingEncoder` 测试正好覆盖这条：

- GPU 返回 `Ok(None)` → CPU 编码器调用次数 **0**、backend 仍为 `gpu-zero-copy`、策略不停用

我修了其中一条断言：`real_error_falls_back_to_cpu` 原来要求「单次 RegisterFailed 就停用」，
与策略的「3 次重试上限」矛盾。改为循环到上限再断言停用。

```
cargo test -p er-client --lib → 11 passed
```

## 四、回归

| 端 | 结果 |
|---|---|
| Rust | **145 passed**（140 → 145） |
| Go 单测 | 16 包全绿 |
| 集成 | 通过 |
| Flutter | 18 passed |

## 五、还没验证的

**`backend=gpu-zero-copy` 仍未在真机出现。** 修的是判定逻辑，但真机要出帧必须桌面有画面变化
（静止时本来就返回 `Ok(None)`，这是正确行为）。

下一轮验证方式：跑跨机会话时让主控发输入事件驱动光标移动（光标移动本身就是画面变化），
或在被控端开一个动画窗口，然后确认日志里 `backend=gpu-zero-copy` 持续保持、且静止时不翻成 cpu。

## 六、ACCESS_LOST 的现状（agent-1 实测）

用 `ChangeDisplaySettings` 切刷新率触发的是 `0x887A0001 DXGI_ERROR_INVALID_CALL`，
**不是 ACCESS_LOST**。已扩成两级自愈（duplication → device 重建 → 重新枚举 adapter），
`rebuilds 0→2 / device_rebuilds=1`，但**重建后长生命周期进程仍拿不到帧，新进程立刻正常**。

所以 `run_access_lost` 维持 **INCONCLUSIVE**，没有为了交差改断言。
真要验证需换触发方式（UAC 安全桌面 / 外接屏热插拔 / RDP 重连），或产品层直接「重建整个 ScreenCapture」。
