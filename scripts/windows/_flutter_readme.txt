EasyRemote · Flutter Windows 客户端（远控 macOS）
============================================

【这是什么】
项目原生的 Flutter 客户端（就是 client/ 那一套 UI：登录页、设备列表、连接页、会话页、
文件页、诊断页、设置页），这次第一次构建出 Windows 桌面版。不是之前那个调试壳子。

【怎么用】
1. 解压到任意目录（不要在压缩包里直接运行）。
2. 双击 Start-EasyRemote.cmd（或直接运行 easyremote_client.exe）。
3. 登录页：手机号随便填（例如 13700008102），短信验证码固定 123456。
4. 进主界面后，点「连接」/「发起远控」，输入：
     设备码：9 位数字（被控端 macOS 上显示的）
     验证码：6 位数字
5. 连上后会话页会显示 macOS 实时画面，鼠标键盘直接可用。

【目录说明】
    easyremote_client.exe   主程序
    flutter_windows.dll     Flutter 运行时（必须同目录）
    er_ffi.dll              Rust 核心（采集/编解码/加密/注入）
    data\                  资源与 Dart 快照（必须同目录）
    Start-EasyRemote.cmd    启动脚本

【已知限制】
- 画面约 1 帧/秒：macOS 侧是 OpenH264 软件编码（1080p 一帧约 400ms）。
- 被控端需要 macOS「屏幕录制」+「辅助功能」两项授权。
- 需要被控端一侧建立服务 + 隧道（当前由 Mac 侧脚本维持，网关地址已烧进本次构建：
  http://127.0.0.1:18090）。

【构建信息】
Flutter 3.50.0 / Dart 3.14.0，release 构建，--dart-define=ER_GATEWAY=http://127.0.0.1:18090
