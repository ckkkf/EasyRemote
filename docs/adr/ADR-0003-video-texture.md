# ADR-0003: 视频零拷贝呈现路线（ER-601a Spike）

- 状态: 提案（未实现；当前仍为 RGBA 拷贝 + Dart 解码）
- 日期: 2026-11-22
- 关联: ER-601、PRD 媒体指标、A-11

## 现状

SessionVideo 每帧：
1. Rust FFI er_ctrl_frame 把 RGBA 拷进 Dart malloc buffer；
2. Dart 复制到 Uint8List；
3. decodeImageFromPixels 再上传/生成 ui.Image；
4. 旧 image dispose。

问题：每帧多次 CPU 拷贝 + Dart 解码，1080p 下无法达到目标帧率，且占用 UI isolate。

## 候选

### A. macOS FlutterTexture + CVPixelBuffer（首选）
- Swift 注册 FlutterTexture，Dart 用 Texture(textureId)。
- Rust 提供 er_ctrl_copy_frame(session, pixelBufferBase) 写入 BGRA；
- FlutterTexture.copyPixelBuffer 返回 CVPixelBuffer，由引擎上传 GPU。
- 仍是 Rust→CVPixelBuffer 一次拷贝，但移除 Dart 分配/解码/Image 生命周期。

### B. 真正零拷贝共享纹理
- macOS IOSurface/Metal texture 由 Rust 采集/解码直接写入，Swift 包装为 CVPixelBuffer；
- Windows D3D11 shared texture；Linux DMA-BUF。
- 跨进程/跨语言句柄与生命周期复杂，工作量最大。

### C. 保持现状并明确 experimental
- 不改呈现路径；UI 显示真实 FPS/降档；
- 仅适合 beta 诊断，不能满足 1080p60。

## 决策

采用 A 作为第一步（native texture，至少消除 Dart 每帧搬运/解码），B 作为后续优化；在 A 完成前不宣称零拷贝。

## 验收计划

- Dart 无 decodeImageFromPixels/每帧 Uint8List 分配（代码扫描 + 单测）；
- Texture 呈现 FPS 与 native decode_fps 差值 ≤10%；
- 1080p30 软解不阻塞输入/控制（输入延迟测试）；
- macOS 实机验证；Windows D3D11 后续。

## 风险

- 平台插件需要 macOS Runner 签名/沙箱权限；
- CVPixelBuffer 生命周期必须与 FlutterTexture 线程安全；
- 失败时必须回退现有 RGBA 路径并显式提示。
