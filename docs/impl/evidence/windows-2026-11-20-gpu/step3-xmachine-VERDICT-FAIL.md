# step3 跨机断言判决（对 20:21 那次运行）

来源日志：step3-xmachine-windows.log（被控端原始输出，未修改）

该次运行的实际结果：
- 链路层：PASS（frames_received=5、frames_sent=8、cursor_after=(399,299)、moved_to_target=true）
- step3 断言：**FAIL**
  原因：日志为 `CODEC: backend=cpu kind=Nvenc 2880x1620`，即编码器是硬件（Nvenc），
  但数据走的是「CPU 回读 → CUDA 上传」，**没有吃到 DXGI 零拷贝**。
  run_xmachine_gpu.cmd 的断言要求 `backend=gpu-zero-copy` 与 `backend_final=gpu-zero-copy`。

根因（lead 定位，已确认）：er-client/src/encoder.rs 原实现把 GPU 返回的 `Ok(None)`
（静止桌面无新帧）当成「没走 GPU」，于是落到 CPU 编码并把 backend 标成 cpu，
且这条路径不打日志 —— 属于语义混淆，不是真正的回退。

结论：**不要把文件里的 `WIN: VERDICT PASS` 当作 step3 通过**。那个 PASS 只覆盖
「链路 + 注入」，不覆盖「零拷贝是否启用」。修复后需重跑并把新日志放这里。
