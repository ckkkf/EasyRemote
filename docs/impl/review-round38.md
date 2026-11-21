# 第三十八轮：零拷贝真机 PASS；输入精度不可测的真因是「显示器断开」

> 日期：2026-11-20 ｜ 主控 agent 亲自执行

## 一、零拷贝：真机通过

```
CODEC: init kind=Nvenc 2880x1620
CODEC: prime_frame=true
CODEC: backend=gpu-zero-copy
CODEC: backend_final=gpu-zero-copy
WIN: frames_sent=8 bytes_total=40330 ms=966 dims=2880x1620
```

同路径此前 3378ms，现在 **966ms（8 帧）**。修掉 `Ok(None)` 语义混淆后，静止桌面不再被误判成回退。

## 二、输入精度失败的真因：显示器是断开的

```
[System.Windows.Forms.Screen]::AllScreens
  WinDisc bounds={X=0,Y=0,Width=1024,Height=768} primary=True
[System.Windows.Forms.SystemInformation]
  MousePresent  = False          ← 关键
query session
  console  Administrator  Active
```

会话 Active，但显示器断开（WinDisc）。系统给了 1024×768 幽灵桌面，`MousePresent=False`。

| 观测 | 解释 |
|---|---|
| `GetSystemMetrics` 返回 2880×1620 | 来自显卡驱动的物理模式 |
| 实际桌面 1024×768 | 断连后的占位桌面 |
| `SetCursorPos(400,300)` 返回 True 但读回 (216,292)/(407,254)/(2423,106) | 没有真实显示器，坐标映射无意义 |
| 每次落点不同 | 不是系统性偏移，是随机 |

**所以「±2px 精度」在显示器断开时根本不可测。** 之前记成「系统性 +4/-2 偏移」是错的。

round30 那次精确命中 (399,299)，是因为当时显示器连着（`screen=1646x926`，真实分辨率的 125% 缩放）。

## 三、排除的干扰项

| 怀疑对象 | 处理 | 结果 |
|---|---|---|
| jiggle 移动光标 | 改用 `scene_move.ps1`（移动窗口不碰光标） | `perturbation_stopped=true`，仍偏移 |
| GameViewer 抢光标 | 临时停 GameViewerService 后重测 | 仍偏移（已恢复服务） |
| DPI 感知时机 | 探针显式 `SetProcessDpiAwarenessContext(-4)` | 返回 True，仍偏移 |

三项全排除，只剩显示器断开这一解释。

## 四、四条断言状态

| 断言 | 状态 |
|---|---|
| `backend=gpu-zero-copy` | ✅ **PASS**（真机日志） |
| 链路收发帧 | ✅ PASS（收 8 / 发 8，2880×1620） |
| 输入精确命中 ±2px | ⛔ **环境不可测**（MousePresent=False） |
| ACCESS_LOST 自愈 | ⚠️ INCONCLUSIVE |

## 五、闭合输入精度需要

任选其一：
1. 接一台显示器（或 HDMI dummy plug 保持显示在线）
2. 保持一个 RDP 会话连接（连接期间 MousePresent=True）
3. 换有真实显示的机器 / 虚拟机

这是物理前提，不是代码问题。

## 六、环境坑记录

1. **日志编码**：`cmd >` 按控制台代码页（GBK）写文件，Rust `eprintln!` 是 UTF-8；读用 `-Encoding Default`，写先在 payload 里 `chcp 65001`
2. **schtasks 重定向**：`/TR "powershell ... > file"` 会被 cmd 解析失败，必须包一层 .cmd
3. **长连接隧道**：ssh 里 `Start-Process udptun` 会随会话回收，必须在 payload 里 `start /b`
4. **hello 文件**：`device_code=` 与 `captcha=` 必须各占一行
