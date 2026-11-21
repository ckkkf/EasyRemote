# 第十一轮：真实采集 · 真实编解码 · D1/D2/D3 · 文件与剪贴板

> 日期：2026-11-20 ｜ 目标轮次 2/20

## 一、采集：从假实现换成真采

`er-capture` 接入 `xcap`，**本机实测抓到 1920×1080 真实帧**：

```
test tests::real_capture_produces_well_formed_frame ... ok
captured 1920x1080 frame
```

- 对外统一 BGRA8，尺寸/字节数校验（`is_well_formed`）
- 静止桌面返回 `Ok(None)` 而不是伪造帧
- 权限拒绝 / 目标丢失 / 不可用三种错误可区分（macOS 屏幕录制权限会明确报 `PermissionDenied`）

## 二、编解码：真实 H.264

`er-codec` 接入 OpenH264：

```
encoded 320x240 frame -> 963 bytes      (Annex-B 起始码校验通过)
roundtrip 160x120 -> 57600 bytes rgb    (编码→解码尺寸一致、像素非全零)
```

顺带修了一个 10 倍性能问题：原来**每帧新建编码器**，实测 160ms/帧；改为跨帧复用后降到 95ms/帧。

## 三、W1 Spike：真实流水线延迟实测（PRD §8 / §7.4）

`cargo run --example pipeline_latency`（1920×1080）：

| 阶段 | 平均 | 峰值 |
|---|---|---|
| 采集（xcap / CoreGraphics 全帧拷贝） | **208.7 ms** | 238.7 ms |
| 编码（OpenH264 软件，1080p） | **94.8 ms** | 159.9 ms |
| **合计** | **303.5 ms** | — |

码率约 7.8 Mbps。

**结论：软件路径距离 40ms 预算差 7 倍。** 这不是实现 bug，是路径选择问题——要达标必须换：
- 采集：ScreenCaptureKit（macOS）/ DXGI Desktop Duplication（Windows），零拷贝
- 编码：VideoToolbox / NVENC 硬件编码

现在有了真实基线数据，替换后可以直接对比。

## 四、D1 / D2 / D3（18 条单测）

| 能力 | 实现 | 关键约束 |
|---|---|---|
| D1 网络诊断 | `diagnose()` 输出结论/原因/影响/建议 | 7 种场景各有可执行建议 |
| D2 防睡眠 | `SleepInhibitor` + RAII `SleepGuard` | Drop 释放，panic 也不留残留 |
| D3 水印 | `layout()` 平铺计算 | 不透明度夹在 0.08–0.15，外扩一圈防裁切 |

## 五、文件传输与剪贴板（13 条单测）

**文件传输**：64 KiB 分块、每块 + 整文件双重 SHA-256、断点续传按连续前缀、乱序块缓冲、重复块幂等。

**剪贴板**：核心是**防回声**——A 改 → 发 B → B 写本地 → B 的监听器又发回 A，会无限循环。用「来源标记 + 内容哈希去重」解决，`no_echo_loop` 测试证明回声被静音、新内容仍能发出。

## 六、验证

| 项 | 结果 |
|---|---|
| Rust 全量 | **64 passed**（原 55 + 新增 9） |
| 其中真实采集 | 通过（1920×1080） |
| 其中真实编解码 | 通过（编码 + 往返） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| vet / gofmt | 无输出 |

## 七、本轮遇到的环境问题

1. **Docker Desktop 仍无法启动** —— PG/Redis 契约测试继续无法执行（第二轮）。
2. **磁盘写满** —— `No space left on device`，Rust 编译失败。清理 Homebrew 缓存与 cargo incremental 后释放到 4.5 GB 可用。
   根因：Flutter（4 GB）+ cargo target（1.8 GB）+ Homebrew 缓存（2.7 GB）叠加。

## 八、下一步

1. 输入注入（PRD §5.3）——键鼠事件注入，可测试
2. ABR 自适应码率（§5.2.4）
3. 隐私屏（§5.2.6）
4. Flutter 客户端六页骨架
5. 硬件编解码与零拷贝采集（需要 40ms 达标）
