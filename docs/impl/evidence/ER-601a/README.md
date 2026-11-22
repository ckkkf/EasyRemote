# ER-601a 证据：纹理路线 ADR

- 状态: 部分完成（ADR；未实现）
- 关联: ER-601、ADR-0003

## 1. 决策

- 先做 macOS FlutterTexture + CVPixelBuffer（native texture，一次拷贝）。
- 真正零拷贝（IOSurface/Metal/D3D11）作为后续。
- 实现前不宣称零拷贝。

## 2. 验收计划

| 断言 | 方式 |
|---|---|
| Dart 无 decodeImageFromPixels/每帧分配 | 代码扫描 + 单测 |
| present FPS 与 native decode_fps 差值≤10% | 会话质量数据 |
| 输入/控制不被解码阻塞 | 延迟测试 |

## 3. 已实现（部分）

- Fact: PresentFps 窗口计数 + SessionVideo.presentFps，与 native decode_fps 可对照。
- Test: TestPresentFpsCountsWindow PASS。

## 4. 未完成

- Swift/Windows 插件、FFI 写 pixel buffer、Dart Texture widget 均未实现。
- 无平台构建验证；仅文档。

## 5. 风险

- macOS 签名/沙箱与纹理线程安全；
- 回退路径必须保留。

## 6. 下一步

实现 macOS native texture（A），再评估真正零拷贝（B）。