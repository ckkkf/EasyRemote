# Spike W1 技术方案：采集 → 编码 → 本机回环

> 配套：[PRD v0.7](../prd/easyremote-prd-v0.7.md) §8 ｜ [协议草案](../spec/protocol.md)
> 周期：第 1 周（7 天） ｜ 状态：待执行 ｜ **本文只做技术方案，不含业务代码**

---

## 1. 本周目标（唯一目标）

**在本机跑通「采集 → 编码 → 解码 → 渲染」，并测出每一段的耗时。**

不做的事：不做网络、不做 UI、不做跨机、不做鉴权。W1 结束只有一个产出——**一张分段时间表**。

### 1.1 验收标准

| 项 | 目标 |
|---|---|
| 采集帧率 | 1080p@60 稳定 ≥ 58 fps |
| 采集耗时 | ≤ 8 ms/帧（P95） |
| 编码耗时 | ≤ 6 ms/帧（P95，1080p60） |
| 解码耗时 | ≤ 5 ms/帧（P95） |
| 渲染耗时 | ≤ 8 ms/帧（P95） |
| 连续运行 | 30 分钟无崩溃、无内存增长（±50MB 内） |
| 画质 | 肉眼无块效应，文字边缘可辨（截图留证） |

> 若采集或编码单项已超预算，**当天停下来查原因**，不要往下走——W2 会把这些误差放大。

---

## 2. 技术选型与理由

### 2.1 屏幕采集：DXGI Desktop Duplication（首选）

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| **DXGI Desktop Duplication** | 零拷贝到 GPU 纹理、延迟最低（约 1 帧）、Windows 8+ 通用 | 独占全屏游戏/受保护内容会失败；不跨 GPU | ✅ **首选** |
| Windows.Graphics.Capture（WGC） | 新、支持窗口级捕获、Win10 1803+ | 延迟略高（约 1–2 帧）、API 复杂、需要 WinRT | 备选（兜底） |
| GDI BitBlt | 兼容性最好 | 慢（10–30ms）、CPU 拷贝、撕裂 | 最后兜底 |

**实现要点**
1. 用 `windows` crate 调 `IDXGIOutputDuplication::AcquireNextFrame`，超时设 **16ms**（约一帧）。
2. **桌面无变化时 DXGI 不产帧**——这是最容易踩的坑。必须实现「静止检测 + 重复上帧」逻辑，否则对端会看到画面冻结。
3. 光标单独处理：`GetFramePointerShape` 拿形状，本地叠加（低延迟），不要烧进画面。
4. 高 DPI：按物理像素采集，不要用逻辑像素，否则文字发虚。
5. 多显示器：Spike 只做主屏，但代码结构要能按 `output_index` 扩展。
6. HDR 显示器：先检测并记录，Spike 阶段不做色调映射。

### 2.2 编码：Media Foundation MFT（Spike 阶段）

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| **Media Foundation（MFT）** | **一套 API 覆盖 NVENC/QSV/AMF**；系统组件，无需分发 SDK；硬件编码器自动注册为 MFT | 抽象层开销、参数不如原生 SDK 细 | ✅ **Spike 首选** |
| 原生 NVENC SDK | 延迟最低、参数最全 | 只支持 N 卡；要打包 DLL；版本兼容麻烦 | W3 若 MF 超标再评估 |
| x264 软编 | 兼容性最好 | 1080p60 吃掉半个 CPU，功耗与延迟都不行 | 仅兜底 |

**为什么 Spike 用 MF**：我们现在的目标是**测出延迟基线**，不是优化到极致。MF 能在一周内跑通三家硬件，NVENC 原生接入至少多花 3–5 天。

**编码参数（低延迟预设）**

| 参数 | 值 | 理由 |
|---|---|---|
| 编码格式 | H.264 High Profile | 兼容性最好；HEVC 留 V1 |
| B 帧 | **0** | B 帧需要重排序，直接增加延迟 |
| lookahead | **关闭** | 同上 |
| GOP | 60（1 秒） | 平衡随机访问与码率 |
| 码率控制 | **CBR** | VBR 会有突发，实时场景要稳定 |
| 码率 | 8 Mbps @1080p60 | 够清晰，留余量 |
| 预设 | low latency / speed | — |
| 切片 | 1 slice/帧（或少 slice） | 减少并行开销 |

### 2.3 解码：MF Video Processor + MFT Decoder

优先 DXVA2/D3D11 硬件解码，与渲染共享同一个 D3D11 设备，避免 CPU 往返。

### 2.4 渲染：D3D11 + DXGI SwapChain

- 用 `DXGI_SWAP_EFFECT_FLIP_DISCARD`，关闭 vsync（Spike 阶段要测真实延迟，vsync 会引入最多 16.7ms 误差）。
- 全屏无边框窗口。
- 后续接 Flutter 时改用外部纹理（`FlutterDesktopGpuSurfaceDescriptor`），那是 MVP 的事。

### 2.5 语言与依赖

| 用途 | crate |
|---|---|
| Win32 API | `windows`（选准 feature 集合，避免编译爆炸） |
| 异步运行时 | `tokio`（W2 传输要用，W1 可以先不用） |
| 日志 | `tracing` + `tracing-subscriber` |
| 时间测量 | `windows` 的 `QueryPerformanceCounter` |
| 图像保存（验证用） | `image` |

---

## 3. 工程骨架（规划，不实现）

```
easyremote/
├─ Cargo.toml                    # workspace
├─ crates/
│  ├─ er-capture/                # 屏幕采集抽象
│  │   └─ src/lib.rs             # trait Capture { fn next_frame() -> Frame }
│  ├─ er-codec/                  # 编码/解码抽象
│  │   └─ src/lib.rs             # trait Encoder / Decoder
│  ├─ er-render/                 # D3D11 渲染
│  ├─ er-transport/              # W2 才实现，W1 只留空 trait
│  └─ er-common/                 # 帧结构、时间戳、错误类型
├─ apps/
│  └─ spike-w1/                  # 本机回环验证程序
│      └─ src/main.rs
└─ docs/
```

**关键设计：所有能力走 trait**，W1 只有本机实现，但接口要为 W2/W3 留好：

```rust
pub trait Capture {
    fn next_frame(&mut self, timeout_ms: u32) -> Result<Option<Frame>>;
    fn info(&self) -> CaptureInfo;   // 分辨率、刷新率、显示器数
}

pub trait Encoder {
    fn encode(&mut self, frame: &Frame) -> Result<EncodedPacket>;
    fn request_keyframe(&mut self);
    fn set_bitrate(&mut self, bps: u32);
}

pub trait Decoder {
    fn decode(&mut self, pkt: &EncodedPacket) -> Result<Option<Frame>>;
}
```

**为什么现在就要 trait**：W3 要换成网络传输、MVP 要接 Flutter，如果 W1 写成「一把梭」，后面全是重构。

---

## 4. 七天安排

| 天 | 任务 | 产出 |
|---|---|---|
| D1 | 工程骨架 + `windows` crate 编译通过 + DXGI 初始化 | 能列出显示器信息 |
| D2 | `AcquireNextFrame` 成功拿到纹理 + 静止检测 | 日志打印帧序号与变化率 |
| D3 | 纹理 → CPU 读回 → 存 PNG | 能截出正确画面（验证采集正确性） |
| D4 | MF H.264 编码器初始化 + 编码单帧 | 编码成功，输出 NALU |
| D5 | 解码 + D3D11 渲染 | 屏幕上看到编码后的画面 |
| D6 | 打点测量：采集/编码/解码/渲染分段耗时 | 初步数据 |
| D7 | 30 分钟长跑 + 数据整理 | **分段时间表** |

---

## 5. 测量方法（必须可复现）

### 5.1 分段打点

```
T0  AcquireNextFrame 返回
T1  纹理 → 编码器输入就绪
T2  编码完成（拿到 NALU）
T3  解码完成（拿到纹理）
T4  Present 返回

采集耗时 = T1 - T0
编码耗时 = T2 - T1
解码耗时 = T3 - T2
渲染耗时 = T4 - T3
```

用 `QueryPerformanceCounter`，单位微秒。每帧记录，跑完导出 CSV。

### 5.2 端到端延迟（真实体感）

分段耗时不等于用户感知延迟。用**相机拍屏法**：

1. 屏幕上跑一个毫秒计时器（60Hz 刷新）。
2. 用 240fps 相机同时拍「源窗口」和「渲染窗口」。
3. 逐帧比对两个窗口显示的毫秒数差。

这是唯一能验证「40ms 目标」的方法，**W3 必须做**，W1 可以先不做（本机回环没有网络段）。

### 5.3 静止检测验证

把鼠标停在桌面不动 10 秒，观察渲染端画面是否冻结（应该有画面但帧率下降）。这是 DXGI 最经典的坑。

---

## 6. 风险与预案

| 风险 | 表现 | 预案 |
|---|---|---|
| DXGI 在独占全屏下失败 | `AcquireNextFrame` 返回 `DXGI_ERROR_ACCESS_LOST` | 实现自动重建 + 后续接 WGC 兜底 |
| 静止时无帧 | 画面冻结 | 重复上帧 + 定时强制刷新 |
| MF 编码器初始化失败 | 找不到 H.264 MFT | 检查系统组件；回退软编验证链路 |
| 硬件编码延迟超标 | 编码 > 10ms | 换 NVENC 原生 SDK；或降分辨率测基线 |
| 高 DPI 下采集尺寸不对 | 画面被裁剪/缩放 | 按物理像素，调用 `SetProcessDpiAwareness` |
| `windows` crate 编译慢/体积大 | 首次编译 10+ 分钟 | 只开必要 feature，用 sccache |

---

## 7. W1 交付物

1. `docs/spike/w1-results.md` — 分段时间表 + 结论
2. 至少 3 张截图：原始采集、编码后渲染、静止状态
3. 工程骨架代码（可编译、可运行）
4. 一句话结论：**这套管线能不能撑住 1080p@60 的 40ms 预算**

---

## 8. W1 结束时必须回答的问题

| # | 问题 | 影响 |
|---|---|---|
| Q1 | 采集 + 编码 + 解码 + 渲染的固定开销是多少？ | 决定留给网络多少预算 |
| Q2 | MF 的抽象开销有多大？是否必须换 NVENC？ | 决定 W2/W3 的编码方案 |
| Q3 | 静止检测处理正确吗？ | 决定用户会不会看到"画面卡住" |
| Q4 | 30 分钟长跑有内存泄漏吗？ | 决定要不要现在处理生命周期 |
| Q5 | 有没有出现无法解释的延迟抖动？ | 决定要不要换采集方案 |

**Q1 的答案如果是「固定开销 > 30ms」，那 40ms 端到端目标就不成立，需要立刻回来改 PRD 的性能基线。**
