# Windows GUI 主控端（er-winapp）· 2026-11-21

## 是什么

一个 Windows 桌面程序（Rust + egui，纯 Rust 无 Flutter/CMake 依赖）：
填设备码 + 验证码 → 看到并操作一台 macOS 的实时画面（鼠标移动/点击/滚轮/键盘转发，
CGEventPost 真注入）。链路：gateway → 信令 → UDP 中继 → ed25519+x25519 握手 → H.264。

## 产物

| 文件 | 说明 |
|---|---|
| `C:\Users\...\Desktop\EasyRemote-Windows.zip` | 分发包（EasyRemote.exe + udptun.exe + er.conf + README + 启动脚本） |
| `winapp-screen.png` | 联调截图（已连接 + 远端 Mac 画面） |
| `release-screen.png` | release 版验证截图 |
| `mac-host-release.log` | 被控端持续推流日志（49 帧） |

## 实现要点（踩到的坑）

1. **窗口 14x14 畸形**：验证机 DPI 175%，逻辑 1280x860 = 物理 2240x1505，
   远超 1646x926 屏幕。winit/eframe 在这种尺寸下会得到 14x14 无标题窗口。
   修法：`.with_maximized(true)`，尺寸交给系统。
2. **界面全是「□」**：egui 自带字体不含 CJK。启动时加载
   `C:\Windows\Fonts\msyh.ttc`（失败退 simhei/simsun/Deng）。
3. **握手校验失败 10054**：app 连 `127.0.0.1:19000`，但 udptun client 没起，
   Windows 对无监听 UDP 回 ICMP port unreachable → 下次 recv 报 WSAECONNRESET。
   修法：启动器必须先拉 udptun client（`Start-EasyRemote.cmd` 已内置）。
4. **encoder: unsupported config**：`mac_host` 的 gop 写死 60，而
   `is_low_latency()` 要求 `gop <= fps`；30fps 下被拒。改成 `gop = fps`。
5. **每帧 8MB 纹理**：解码后按 2 倍最近邻降到 1280 宽再上传，
   否则 UI 内存带宽吃满。

## 已验证

- 连接：设备码 + 验证码 → 会话 → 握手 → 推流（`已连接 · 960x540`）
- 画面：Windows 窗口内实时显示 macOS 桌面（截图见 winapp-screen.png）
- 输入：`mac_host` 统计 `injected=544`（鼠标移动事件确实注入到 macOS）

## 当前限制

- **约 1 fps**：macOS 侧 OpenH264 软编 1080p 约 400ms/帧，且主控端解码限流 80ms/帧。
  要流畅需接 VideoToolbox 硬编（后续）。
- 被控端需 macOS「屏幕录制」+「辅助功能」授权，缺一项 → 黑屏/鼠标不动。
- 依赖 Mac 侧 `scripts/mac/start_host.sh` 维持服务 + ssh -R 隧道。
