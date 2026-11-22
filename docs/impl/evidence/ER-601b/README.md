# ER-601b 证据：native 纹理（未编译验证）

- 状态: 部分完成（代码 + Dart 回退测试；Xcode 缺失无法验证 Swift）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-ffi/session.rs | er_ctrl_frame_bgra |
| VideoTexturePlugin.swift | FlutterTexture + CVPixelBuffer + 60Hz FFI |
| MainFlutterWindow.swift | 注册插件 |
| video_texture.dart | create/dispose 桥，异常回退 |
| session_video_io.dart | textureId/attachTexture/close 释放 |
| session_page.dart | Texture 优先，RawImage 回退 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 无平台插件时不抛错、回退 | TestTextureBridgeFallsBackWithoutPlugin | PASS |
| 既有 E2E 不回归 | host/session/lossy E2E | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |
| macOS 原生编译 | flutter build macos | **未执行（Xcode not installed）** |

## 3. 未验证/风险

- Swift 代码未编译，符号/API 可能需调整（registrar.textures、FlutterTexture 签名）。
- CVPixelBuffer 生命周期/线程安全未实机验证。
- 纹理路径仍有一次 BGRA 拷贝，不是零拷贝。
- Windows D3D11 纹理未实现。
- Xcode 缺失是当前环境阻塞；需在 macOS CI/开发机验证。

## 4. 下一步

安装 Xcode 后：flutter build macos → 实机跑会话 → 对比 presentFps/native decode_fps；修正 Swift 编译/生命周期。之后评估真正零拷贝（IOSurface/Metal）。
