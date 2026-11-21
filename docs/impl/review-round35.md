# 第三十五轮：step3 实跑（结论与一个未解问题）

> 日期：2026-11-20 ｜ 由主控 agent 亲自执行，非子代理转述

## 一、先纠正一个错误结论：机器根本没锁

上一轮我根据「LogonUI.exe 进程存在」判断被控机锁屏，据此让你去解锁。**这个判断是错的。**

实际测量：

```
DXGI_LOCKSTATE adapter="NVIDIA GeForce RTX 3060 Laptop GPU" size=2880x1620 format=87 mode=Some((2880,1620,120))
DXGI_LOCKSTATE is_locked=false
RESULT: PASS (is_locked=false)
```

`OpenInputDesktop` 返回的桌面名是 `Default`（未锁屏）。LogonUI.exe 在解锁状态下也会常驻，用它做判据必然误判。

**修正**：四个 step3 脚本与 `scripts/mac/step3_xmachine.sh` 里的 LogonUI 门禁已全部移除，改由 session 1 里的 payload 自己报 `is_locked`。

## 二、四项实测结果

| 脚本 | 结果 | 关键数据 |
|---|---|---|
| `run_lockstate` | **PASS** | `is_locked=false`，2880×1620@120Hz |
| `run_bench_compare` | **PASS** | copy 5.18ms/8.38ms/69.7fps vs zero-copy 0.34ms/7.20ms/130.3fps |
| `run_access_lost` | **INCONCLUSIVE** | 显示器模式切换是空操作，未触发 ACCESS_LOST（脚本如实报告，未编造 PASS） |
| `run_xmachine_gpu` | **PASS（但见下）** | 收 5 帧 1920×1080 / 发 8 帧 2880×1620 / 注入 3 事件 / 光标 (1642,961)→(399,299) |

## 三、跨机会话 PASS，但**零拷贝没生效**

被控端日志：

```
CODEC: backend=cpu kind=Nvenc 2880x1620
CODEC: backend_final=cpu
```

`kind=Nvenc` 说明硬件编码器确实起来了，但 `backend=cpu` 说明**走的是 CPU 回读路径，不是零拷贝**。

更关键：**没有打印任何回退原因**。设计上「可用→失效」时应打一行 `[er] 零拷贝路径不可用，回退 CPU: <reason>`，实际日志里没有。

这是一个真实缺陷：静默回退。用户会看到「帧能出来、分辨率对」，但完全不知道性能路径没走对。

## 四、本轮踩的坑（都修了，但值得记）

1. **隧道随 ssh 会话退出被回收**：`ssh win "... Start-Process udptun"` 启动的进程在 ssh 断开后消失，导致被控端 UDP 报 10054（连接被重置）。改为在 schtasks payload 里 `start /b` 启动，才常驻。
2. **hello 文件格式**：主控按行解析 `device_code=` 与 `captcha=`，两字段必须各占一行；写成一行会报 `missing captcha=`。
3. **脚本参数引号**：`run_xmachine_gpu.cmd` 从 Mac 传参时被 cmd 拆错，报「此时不应有 =13700...」。最终改为直接写 payload，绕过参数拼装。

## 五、验证

| 项 | 结果 |
|---|---|
| Windows `cargo test --workspace` | 141 passed（子代理基线） |
| macOS `cargo test --workspace` | 140 passed |
| Flutter | 18 passed |
| 跨机会话（真机） | PASS |
| **零拷贝真机生效** | **❌ 未达成（backend=cpu）** |

## 六、下一步（明确）

1. **查清为什么 backend=cpu**：是 `dxgi_mut()` 拿不到、还是尺寸/注册失败；并且**必须把回退原因打进日志**
2. 触发一次真正的 ACCESS_LOST（Win+P 切投影 / 插拔外接屏），验证自愈
3. 零拷贝生效后再重跑一次跨机，拿到 `backend=gpu-zero-copy` 才算这条完成
