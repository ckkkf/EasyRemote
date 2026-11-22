# ER-601 证据：解码统计与去限帧（部分）

- 状态: 部分完成（零拷贝未实现）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| ctrl_session.rs | 删除 80ms 限帧；FpsCounter；decode_fps/decode_ms/dropped |
| er-ffi session.rs | er_ctrl_info 增加 fps/decode_ms/dropped |
| session_video_io.dart | nativeFps/decodeMs/droppedFrames |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| FPS 按 1 秒窗口统计 | fps_counter_reports_window_rate | PASS |
| 无 12fps 人为限帧 | 代码路径（删除 80ms 判断） | 已改 |
| 既有 E2E 不回归 | host/session/lossy E2E | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成（核心差距）

- **零拷贝 texture 未实现**：仍是 RGB→RGBA 拷贝 + Dart decodeImageFromPixels + 每帧 ui.Image。
- 无硬件解码；软解 1080p 仍 ~10-15fps。
- 未实现 native/present FPS 一致性断言。
- 未做 30 分钟管线稳定性。

## 4. 下一步

ER-601a：Flutter 外部纹理/平台通道（macOS Metal/Windows D3D）实现零拷贝呈现；否则保持 experimental 并在 UI 明确实际 FPS。
