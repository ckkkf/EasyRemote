# Windows 主控 → macOS 被控（step4）证据（2026-11-20）

## 这一轮要证明什么

PRD v0.8 §4.1 把 macOS 划在 V1（MVP 只做 Win↔Win），而此前唯一跑通的跨机组合是
**Mac 主控 → Windows 被控**（见 windows-2026-11-20-gpu/）。本目录补齐反方向：
**Windows 发输入 → macOS 真注入 + macOS 真实屏幕帧回到 Windows**。

## 代码变更

| 文件 | 变更 | 为什么 |
|---|---|---|
| core/crates/er-input/src/macos.rs | 新增 MacDisplay（像素↔points 换算）、MacInjector 记录 mapped_moves、光标按像素回读 | 采集是像素、CGEvent 是 points；Retina 上直接当同一坐标会偏一半 |
| core/crates/er-capture/src/lib.rs | macOS 的 info 尺寸改用 CGDisplayPixelsWide/High | xcap 的 width() 返回 points，capture_image 返回像素，Retina 上编码器会 DimensionMismatch |
| core/crates/er-client/examples/mac_target.rs | 新增：macOS 被控端（采集/编码/注入/按像素断言 + 授权自检） | 之前没有 macOS 被控端可执行 |
| core/crates/er-client/examples/win_controller.rs | 新增：Windows 主控端（发帧/发输入/收帧） | 之前没有 Windows→Mac 方向的主控端 |
| scripts/mac/step4_win2mac.sh | 新增：Mac 侧编排（服务 + 反向隧道 + 拉被控端 + 断言） | 与 step3 镜像 |
| scripts/windows/run_win2mac.cmd | 新增：Windows 侧主控编排（udptun + win_controller） | 同上 |

## 本机回环验证（macOS 被控端自身正确性）

同一台 Mac 上跑 mac_target（被控）+ 已有 xmachine_controller（主控），验证
"会话 → 双向帧 → 注入 → 按像素回读" 全链路。日志：
loopback-mac-target.log / loopback-mac-controller.log

关键行：
```
MAC: display pixels=1920x1080 points=1920x1080 origin=(0,0) scale=1
MAC: permission_screen=ok (info=1920x1080 frame=1920x1080 nonzero_samples=8317)
MAC: frames_received=5 resolution=1920x1080 first_h264_bytes=86285
MAC: frames_sent=5 bytes_total=106222 ms=2081
MAC: injected_moves=[(400, 300), (363, 321), (400, 300)]
MAC: injected_mapped=3
MAC: cursor_after=Some((400, 300))
MAC: moved_to_target=true (target=Some((400, 300)) actual=Some((400, 300)) delta=(0, 0))
MAC: VERDICT PASS
```

## 跨机验证（2026-11-21 10:49，PASS）

执行：`bash scripts/mac/step4_win2mac.sh`（Mac 侧编排 + Windows 侧 run_win2mac.cmd）。
日志：mac-target.log（被控端）/ win-controller.log（主控端）/ step4-hello.txt。

```
MAC: display pixels=1920x1080 points=1920x1080 origin=(0,0) scale=1
MAC: permission_screen=ok (info=1920x1080 frame=1920x1080 nonzero_samples=8317)
MAC: frames_received=5 resolution=320x240 first_h264_bytes=762
MAC: frames_sent=5 bytes_total=93945 ms=1961
MAC: injected_moves=[(400, 300), (363, 321), (400, 300)]
MAC: injected_mapped=3
MAC: cursor_baseline=Some((222, 59)) recheck=Some((222, 59)) stable=true
MAC: cursor_after=Some((400, 300))
MAC: moved_to_target=true (target=Some((400, 300)) actual=Some((400, 300)) delta=(0, 0))
MAC: RESULT screen_permission=true frames_received=5 frames_sent=5 input_events=3 moved_to_target=true elapsed_ms=14925
MAC: VERDICT PASS
```

```
WIN: capture_ok 1024x768
CODEC: init kind=Nvenc 1024x768
WIN: capture_send_failed (capture: capture unavailable); fallback=synthetic_h264
WIN: frames_sent=5 bytes_total=5701 path=synthetic
WIN: input_events_sent=3 target=(400,300) sent_before_recv=true
WIN: frames_from_target=5 resolution=1920x1080 first_h264_bytes=74651
WIN: RESULT frames_sent_target=5 frames_from_target=5 input_events_sent=3 frames_path=synthetic elapsed_ms=7350
WIN: VERDICT PASS
```

结论：**Windows 发出的 3 个鼠标事件在 macOS 上真注入，光标精确落到 (400,300)（误差 0px）；
macOS 的真实 1920x1080 屏幕帧（93945 字节 H.264）被 Windows 收到并解码成功。**

### 两个必须说清的边界

1. **Windows 主线画的是合成帧**：主控端跑在 SSH 会话（Session 0），DXGI 能 open 但抓不到
   真实桌面（`capture_send_failed: capture unavailable`），代码按设计回退合成帧。
   这不影响结论——被控端只负责解码显示，主控发什么画面与"能否控制 Mac"无关；
   要主控也发真实 Windows 桌面，把 win_controller 放进交互会话（schtasks /IT）即可。
2. **macOS 侧授权是硬前提**：`permission_screen=ok` 已断言；辅助功能授权由
   `moved_to_target=true` 间接证明（未授权时 CGEventPost 被静默丢弃，光标不会动）。

### 排障记录（真机踩到的两个坑）

- `ssh win "cmd /c C:\...\run_win2mac.cmd <arg>"` 在这台 Windows 的 sshd 上会多带一个尾引号，
  导致 `@echo off` 解析失败（日志是 `"echo." 不是内部或外部命令`）。改成直接调用
  `ssh win "C:\EasyRemote\run_win2mac.cmd <arg>"` 即正常。
- Session 0 下 `open_primary()` 返回 1024x768 假桌面但 `capture_and_encode` 报错；
  主控端原先 `.expect()` 直接 panic，把整轮会话带死。已改成"采集失败 → 合成帧"的无损回退。

## 复现方式

```bash
# 前提：Windows 验证机在线；core 两端已构建
bash scripts/mac/step4_win2mac.sh
```

脚本会把 mac-target.log / win-controller.log 落到本目录，并按
permission_screen=ok、moved_to_target=true、双方 VERDICT PASS 断言。

---

## 可视化演示（2026-11-21 13:10，DEMO PASS）

`bash scripts/mac/demo_win2mac.sh 3` 一条命令跑完，产物在 `docs/impl/evidence/win2mac-demo/`。

### 演示做了什么

1. **输入方向**：Windows 主控画一个圆（75 个鼠标事件，3 秒）→ macOS 被控端
   `CGEventPost` 实时注入。跑脚本时盯着 Mac 屏幕，能看到光标自己沿圆转圈。
2. **画面方向**：注入结束后 macOS 采集自己的真实 1920x1080 屏幕，编码 30 帧回传；
   Windows 端落盘 H.264 裸流（113560 字节），Mac 侧 ffmpeg 转成 mp4 并抽帧。

### 产物

| 文件 | 说明 |
|---|---|
| `demo_mac.mp4` | **Windows 端录下的 macOS 桌面**（30 帧 @25fps，1920x1080） |
| `demo_mac_trajectory.png` | 把被控端实际注入的 75 个坐标画在 Mac 真实桌面上（红圈） |
| `demo_mac_01..05.png` | 回传码流的关键帧截图 |
| `demo_mac.h264` | Windows 端原始录像（Annex-B H.264） |
| `mac-target.log` / `win-demo.log` | 两端原始日志 |

### 关键断言行

```
MAC: injected_moves=[(1180, 540), (1179, 558), ... (1010, 326)]   # 75 个点构成一个圆
MAC: moved_to_target=true (target=Some((1010, 326)) actual=Some((1010, 326)) delta=(0, 0))
MAC: frames_sent=30 bytes_total=113560 ms=12281 (after_input=true)
MAC: VERDICT PASS
WIN: demo_moves_sent=75 circle_center=(960,540) r=220 secs=3
WIN: recorded_frames=30 bytes=113560 path=C:\EasyRemote\demo_mac.h264
WIN: VERDICT PASS
```

### 怎么自己看

```bash
bash scripts/mac/demo_win2mac.sh 5      # 5 秒画圆，跑的时候看 Mac 屏幕上的光标
open docs/impl/evidence/win2mac-demo/demo_mac.mp4
```

### 边界

- Mac 侧采集用 `CGWindowListCreateImage`，**不包含鼠标指针**，所以录像里看不到光标；
  光标轨迹用 `demo_mac_trajectory.png` 叠加展示。
- Windows 主控端仍是 CLI（Flutter Windows 客户端未构建），录像是在 Windows 上落盘后
  传回 Mac 播放的，不是「隔着网络实时看到画面」。要实时预览得写一个 Windows 窗口程序。

