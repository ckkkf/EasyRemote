# 第四十三轮：Windows 主控 → macOS 被控（补齐反向跨机）

> 日期：2026-11-20 ｜ 机器：Mac（macOS 26/Aqua）+ WIN-VERIFY-PC（Windows 11，验证机当前离线）

## 一、这轮做了什么

此前唯一跑通的跨机组合是 **Mac 主控 → Windows 被控**（step3，有真机日志）。
PRD v0.8 §4.1 把 macOS 放在 V1，但反方向的代码从来没写过：没有 macOS 被控端，
也没有 Windows 主控端。这轮补齐：

| 新增/修改 | 作用 |
|---|---|
| `core/crates/er-client/examples/mac_target.rs` | macOS 被控端：开会话 → 收主控帧 → 采集本机屏编码回传 → 收输入 CGEventPost 真注入 → **按像素**回读光标断言 |
| `core/crates/er-client/examples/win_controller.rs` | Windows 主控端：读 hello → 发起连接 → 发帧 → 发输入 → 收 Mac 真实屏幕帧 |
| `core/crates/er-input/src/macos.rs` | `MacDisplay`：像素↔points 换算（含主屏原点）+ `injected_mapped` 计数 + `cursor_pixel()` |
| `core/crates/er-capture/src/lib.rs` | macOS 的采集信息尺寸改用 `CGDisplayPixelsWide/High` |
| `scripts/mac/step4_win2mac.sh` | Mac 侧编排：服务 + `ssh -R` 反向隧道 + 拉被控端 + 断言 |
| `scripts/windows/run_win2mac.cmd` | Windows 侧编排：udptun client + win_controller |

## 二、三个必须修的坑（都不是"没写"，是写了会静默错）

### 1. Retina：采集是像素，CGEvent 是 points

`xcap` 在 macOS 上 `Monitor::width()` 返回 `CGDisplayBounds`（points），
而 `capture_image()` 返回 `CGImageGetWidth`（像素）。Retina 上两者差 2 倍：

- 用 points 配编码器 → `DimensionMismatch`（编码器按 points 分配缓冲，帧是 2 倍大）
- 用像素直接当 CGEvent 坐标 → 光标只走到屏幕左上角的一半处，表现为「注入完全不生效」

修法：`MacDisplay::{pixel_to_point, point_to_pixel}` 统一换算，采集侧 info 也统一到像素。
非 Retina（scale=1）时两者恰好相等——**这正是它容易被漏掉的原因**。

### 2. 中继只转发「两端都已注册」的包

被控端 `begin` 后立刻 `announce`，而主控端 `request_session` 之后还要 sleep 1.5s 才注册，
被控端的握手就发进了真空，双方各等 15s 超时。本机回环第一次跑就是这样：

```
MAC: handshake_verified (ed25519 + x25519)
thread main panicked: recv frames: AgentError("no frames decoded")
MAC: handshake: AgentError("no peer handshake within 15s")
```

注意「一方成功、另一方超时」这种非对称失败——单看主控日志会误判成对端没起来。
修法：被控端在 `begin` 与 `announce` 之间等到 2.5s，与主控 announce 的时序对称。

### 3. 授权缺一项就变成"链路通但画面黑/光标不动"

macOS 被控端需要两项系统授权（屏幕录制 + 辅助功能），缺任一项都不会报错，只会静默失效。
所以 `mac_target` 在开会话**之前**先自检，并把结论打进日志（`permission_screen=ok/denied`、
`cursor_baseline_pixel=Some/None`），断言里直接带 `screen_permission=true`。

## 三、本机回环实测（macOS 被控端自身正确性）

同一台 Mac 跑 `mac_target`（被控）+ 已有 `xmachine_controller`（主控）：

```
MAC: display pixels=1920x1080 points=1920x1080 origin=(0,0) scale=1
MAC: permission_screen=ok (info=1920x1080 frame=1920x1080 nonzero_samples=8317)
MAC: frames_received=5 resolution=1920x1080 first_h264_bytes=86285
CODEC: backend_final=cpu
MAC: frames_sent=5 bytes_total=106222 ms=2081
MAC: injected_moves=[(400, 300), (363, 321), (400, 300)]
MAC: injected_mapped=3
MAC: cursor_after=Some((400, 300))
MAC: moved_to_target=true (target=Some((400, 300)) actual=Some((400, 300)) delta=(0, 0))
MAC: RESULT screen_permission=true frames_received=5 frames_sent=5 input_events=3 moved_to_target=true elapsed_ms=9377
MAC: VERDICT PASS
```

证据：`docs/impl/evidence/win2mac-2026-11-20/loopback-*.log`。

## 四、跨机真机验证（2026-11-21 10:49，PASS）

`bash scripts/mac/step4_win2mac.sh` 一把过。主控端（Windows）与被控端（macOS）两端日志：

```
MAC: frames_received=5 resolution=320x240 first_h264_bytes=762
MAC: frames_sent=5 bytes_total=93945 ms=1961
MAC: injected_moves=[(400, 300), (363, 321), (400, 300)]
MAC: cursor_baseline=Some((222, 59)) recheck=Some((222, 59)) stable=true
MAC: cursor_after=Some((400, 300))
MAC: moved_to_target=true (target=Some((400, 300)) actual=Some((400, 300)) delta=(0, 0))
MAC: VERDICT PASS

WIN: capture_send_failed (capture: capture unavailable); fallback=synthetic_h264
WIN: frames_sent=5 bytes_total=5701 path=synthetic
WIN: input_events_sent=3 target=(400,300) sent_before_recv=true
WIN: frames_from_target=5 resolution=1920x1080 first_h264_bytes=74651
WIN: VERDICT PASS
```

即：**Windows 发鼠标 → macOS 真注入（落点误差 0px）→ macOS 真实 1920x1080 屏幕帧回到 Windows 并解码成功。**

### 排障记录（真机踩到的两个坑，都已修）

1. `ssh win "cmd /c C:\...\run_win2mac.cmd <arg>"` 在这台 Windows 的 sshd 上会多带一个尾引号，
   把 `@echo off` 顶掉（日志表现为 `"echo." 不是内部或外部命令`，整段脚本没执行）。
   改成 `ssh win "C:\EasyRemote\run_win2mac.cmd <arg>"`（默认 shell 就是 cmd）即正常。
2. Session 0 下 `ScreenCapture::open_primary()` 返回 1024x768 假桌面、但 `capture_and_encode`
   报 `capture unavailable`；主控端原来的 `.expect("send frames")` 直接 panic 把整轮会话带死。
   已改成「采集失败 → 合成帧」无损回退（链路/编码/加密/解码完全一致），日志显式打
   `path=synthetic`。

### 边界（不粉饰）

- **Windows 主控发的画面是合成帧**：主控端跑在 SSH 会话，DXGI 抓不到真实桌面。
  这不影响「能否控制 Mac」——被控端只解码显示；要主控也发真实 Windows 桌面，
  把 `win_controller` 放进交互会话（schtasks /IT）即可，代码不用改。
- macOS 侧两项授权是硬前提：`permission_screen=ok` 显式断言，辅助功能由
  `moved_to_target=true` 间接证明（未授权时 CGEventPost 被系统静默丢弃，光标不会动）。

### 此前的外部阻塞（已解除，留档）

2026-11-20 23:14 至 2026-11-21 10:38，Windows 验证机离线：frp 入口 39100/39101/3389
全部 accept 后立即断开（`kex_exchange_identification: Connection closed by remote host`）。
机器开机后 10:38 恢复，本轮验证在此之后完成。

## 五、回归

| 端 | 结果 |
|---|---|
| `bash scripts/gate.sh` | **PASS** |
| Rust `cargo test --workspace` | **165 passed / 0 failed** |

## 六、仍需注意

1. **Windows 主控发的画面是合成帧**（Session 0 限制），输入方向与被控画面都是真的；
   要让主控也发真实 Windows 桌面，把 `win_controller` 放进交互会话跑即可。
2. macOS 编码仍是 OpenH264 软件编码：1920x1080 编码 5 帧耗 2081ms（≈416ms/帧），
   产品级要换 VideoToolbox；这是 macOS 侧性能的第一瓶颈。
3. `MacDisplay::scale()` 对双屏分辨率不一致取均值，属于保守近似；单屏/镜像场景正确，
   双屏异步分辨率需要按显示器各自几何算（V2 多屏切换时再做）。
4. `win_controller` 的 **Windows 真机编译仍待验证**（Mac 上 `cargo check --target x86_64-pc-windows-msvc`
   卡在 `ring` 的 C 依赖缺 Windows SDK 头文件）。已做的替代验证：复制一份去掉
   `#[cfg(target_os = "windows")]` 闸门的同源文件，在 mac 上 `cargo check --example` 全量类型检查通过——
   该 example 不直接调任何 Windows API，调用的 `ScreenCapture::open_primary` 在 Windows 分支已在 step3
   真机跑过，所以编译风险低，但**最终以 Windows 上 `cargo build` 为准**。
