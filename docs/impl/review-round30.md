# 第三十轮：Windows 侧三项落地（依赖 / NVENC / 跨机真实会话）

> 日期：2026-11-20 ｜ 目标：Windows 侧剩余三项一次做完并留可复现证据
> 机器：WIN-VERIFY-PC（Windows 11 专业工作站版 26200，RTX 3060 Laptop，驱动 610.88）
> 接入：ssh win（<frp-host>:<port> → 本机 22，frp）
> 约束：**全程未启用任何代理/VPN**（实测 warp-cli status = Disconnected，http_proxy/https_proxy 为空）
> 原始输出：docs/impl/evidence/windows-2026-11-20/

---

## 一、Windows 依赖获取：打通，cargo build 与 cargo test 全绿

### 1.1 真正的原因（这一条是关键，前两轮卡在这）

Windows 上 cargo 拉索引一律报：

    warning: spurious network error: [7] Could not connect to server
    (Failed to connect to rsproxy.cn:443 over proxy 127.0.0.1 after 2043 ms)

排查结论：机器里 HKCU 的 Internet Settings 残留 ProxyServer=127.0.0.1:9000
（ProxyEnable=0，但 cargo 的 libcurl 仍然按它走），而该端口上没有代理在监听。
**不是镜像不可用，也不是网络不通** —— curl.exe 直连 rsproxy.cn 是 200 / 149ms。
另外 github.com 在该机不可达（20s 超时），所以任何 git 依赖都不可用。

### 1.2 修法：仓库内固化 cargo 配置

新增 core/.cargo/config.toml：

- [source.crates-io] replace-with = "rsproxy-sparse" → sparse+https://rsproxy.cn/index/
  （实测 rsproxy 149ms，index.crates.io 1514ms）
- [http] proxy = ""  → **强制直连**，绕开系统里那个失效的 127.0.0.1:9000
- [net] git-fetch-with-cli = true → 兼容可能的 git 依赖

验证（Windows，先在 vcvars64.bat 环境里）：

    > cargo fetch
      Downloaded openh264 v0.6.6 (registry rsproxy-sparse)
      ...
    EXITCODE_FETCH_OFF=0

### 1.3 构建与测试（原始输出见证据文件）

    > cargo clean && cargo build --workspace          # 干净重建（证据 cargo-clean-build-windows.log）
         Removed 6335 files, 4.1GiB total
         Compiling proc-macro2 v1.0.107
         ...（188 个 crate）
         Finished dev profile [unoptimized + debuginfo] target(s) in 35.98s
    BUILD_EXIT=0

    > cargo test --workspace
    test result: ok. 9 passed; 0 failed     (er-common)
    test result: ok. 8 passed; 0 failed
    test result: ok. 13 passed; 0 failed
    test result: ok. 5 passed; 0 failed
    test result: ok. 13 passed; 0 failed
    test result: ok. 18 passed; 0 failed
    test result: ok. 5 passed; 0 failed
    test result: ok. 17 passed; 0 failed     (er-input，含 Windows 注入单测)
    test result: ok. 35 passed; 0 failed
    test result: ok. 10 passed; 0 failed
      + nvenc::tests::probe_never_panics ... ok
      + nvenc::tests::rejects_non_low_latency_config ... ok
    TEST_EXIT=0

汇总：**10 个 suite、135 条测试全过，0 失败**。

说明：测试跑在 SSH 会话（Session 0）。需要交互桌面的用例本来就不在单测里，
交互会话下的验证见第二、三节（走 schtasks /IT）。

### 1.4 构建前置（Windows 一次性）

- MSVC：C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools（MSVC 14.44.35207）。
  构建前必须先跑 vcvars64.bat，否则 rustc 找不到 link.exe。
  仓库侧用 mp.cmd 包装：call vcvars64.bat >nul & %*
- openh264-sys2 需要 C++ 编译器（cc 通过 vswhere 找到 MSVC）；**不需要 nasm**
  （缺 nasm 时该 crate 自动退化为纯 C 实现）。

---

## 二、NVENC 硬件编码：实现 + 真机验证

### 2.1 实现方式：运行时绑定，不依赖 CUDA SDK

- 新增 core/crates/er-codec/src/nvenc/
  - bindings.rs：bindgen 从仓库内 vendored 的 third_party/ffnvcodec/nvEncodeAPI.h（NVENC API 13.1）生成
  - mod.rs：NvencEncoder + probe()
- 链接方式：**运行时 LoadLibraryA("nvEncodeAPI64.dll") + NvEncodeAPICreateInstance**，
  构建机不需要 NVIDIA SDK；目标机装驱动就有这个 DLL
  （实测 C:\Windows\System32\nvEncodeAPI64.dll，1068264 字节）。
- 设备类型 NV_ENC_DEVICE_TYPE_CUDA：nvcuda.dll 建 CUDA 上下文 → cuMemAllocPitch 分配输入缓冲
  （pitch 自己掌握）→ cuMemcpy2D 上传 BGRA → nvEncRegisterResource / nvEncMapInputResource
  → nvEncEncodePicture（同步模式，enableEncodeAsync=0，单帧即取即走）。
- 低延迟参数：H.264 High、P3 预设 + NV_ENC_TUNING_INFO_ULTRA_LOW_LATENCY、frameIntervalP=1（无 B 帧）、
  CBR、每个 IDR 带 SPS/PPS（repeatSPSPPS=1）。
- 运行时启用策略：er_codec::create_encoder(cfg) 在 Windows 上先试 NvencEncoder::new
  —— **真的把会话开出来**才算可用，失败回退 OpenH264；其它平台直接软件编码。
- 探测接口 er_codec::nvenc::probe()：显卡名（NVML）、驱动版本、驱动支持的最大 API 版本、
  H.264 编码 GUID 是否受支持。

### 2.2 真机输出（schtasks /IT 交互会话，真实屏幕帧 2880×1620）

    === 环境 ===
    gpu             = Some("NVIDIA GeForce RTX 3060 Laptop GPU")
    driver          = Some("610.88")
    nvenc_api_max   = 0x000000d1
    nvenc_available = true
    probe_detail    = nvEnc session opened, H.264 encode GUID supported
    hardware_available = true
    frame_source=screen size=2880x1620 bytes=18662400

    --- 硬件路径: Nvenc ---
    hw encode: 7.824 ms/frame, 127.8 fps, idr=11227 bytes last=1021 bytes

    --- 软件路径: Software ---
    sw encode: 501.719 ms/frame, 2.0 fps, idr=51283 bytes last=106 bytes

    --- 解码校验 ---
    hw decoded: 2880x1620 (rgb bytes 13996800)
    sw decoded: 2880x1620 (rgb bytes 13996800)

    === 对比 ===
    Nvenc/SW            7.824       127.8
    OpenH264          501.719         2.0
    speedup = 64.12x

    RESULT: hw_kind=Nvenc hw_dims_ok=true sw_dims_ok=true dims_match=true
    VERDICT: PASS

同一帧、同一配置、同一进程内交替测：
**硬件 7.8ms/帧（127.8fps）vs 软件 501.7ms/帧（2.0fps）**，
两路 H.264 都被同一个解码器解回 **2880×1620**，尺寸与原帧一致。
另做过一组 1280×720：1.90ms vs 90.15ms（47×）。软件路径受 CPU 负载影响波动较大，两次都在证据里。

### 2.3 端到端也用上了

跨机会话里被控端的编码器日志：

    WIN: capture_display=2880x1620
    CODEC: kind=Nvenc 2880x1620
    WIN: frames_sent=8 bytes_total=55414 ms=5430 dims=2880x1620

即 NVENC 已在真实会话路径上跑，而不是只在 bench 里。

---

## 三、跨机真实会话：Mac 主控 ↔ Windows 被控（全链路跑通）

### 3.1 拓扑（两端真机，中间是真实服务）

    Mac (主控)                                  Windows WIN-VERIFY-PC (被控, Session 1 交互)
    ┌────────────────────────────┐              ┌──────────────────────────────────────┐
    │ stack (gateway+rendezvous) │◄── TCP 18090 ┤ ssh -R 18090 -> 127.0.0.1:18090        │
    │   :18090                   │              │ (SSH 反向隧道，经 frp 承载)           │
    │ relay (UDP) :19000         │◄── UDP ◄─────┤ udptun client :19000 -> TCP :19001    │
    │ udptun server :19001       │◄── TCP 19001 ┤ ssh -R 19001 -> 127.0.0.1:19001        │
    │ xmachine_controller        │              │ xmachine_target (schtasks /IT)        │
    └────────────────────────────┘              └──────────────────────────────────────┘

- 两端不在同一网段（Mac 出口 103.151.172.151，Windows 出口 223.98.219.8），
  且**没有开任何代理/VPN**；用 ssh -R 把 Mac 的 gateway 与中继入口映射到 Windows 的 127.0.0.1。
- 中继是 UDP，SSH 不能直接转发 UDP，所以用仓库已有的 server/cmd/udptun
  （UDP-over-TCP 隧道，每个 UDP 源地址一条 TCP）；udptun 不解析中继协议，只搬字节。
- **被控端必须跑在交互会话**：SSH 登录的进程在 Session 0，没有交互桌面
  （抓屏是 1024×768 假桌面、SendInput 被系统静默拒绝、GetCursorPos 失败）。
  用 schtasks /create ... /ru Administrator /it + schtasks /run 放进 Session 1。

### 3.2 被控端原始输出（Windows target.log）

    TARGET_START 2026/11/20 18:29:01.48
    WIN: gateway=http://127.0.0.1:18090 relay=127.0.0.1:19000 signal=http://127.0.0.1:18090
    WIN: pid=34780 hardware_encoder_available=true
    WIN: device_id=dev-e1a012e1088dd801 device_code=368035388 captcha=894718 (655ms)
    WIN: waiting_for_signalling ...
    WIN: session=ses-7d8574b1323296a0
    WIN: consent=authorized
    WIN: relay_registered local=127.0.0.1:57955
    WIN: handshake_verified (ed25519 + x25519)
    WIN: frames_received=5 resolution=1920x1080 first_h264_bytes=67718
    WIN: capture_display=2880x1620
    CODEC: kind=Nvenc 2880x1620
    WIN: frames_sent=8 bytes_total=55414 ms=5430 dims=2880x1620
    WIN: screen=1646x926 cursor_before=Some((902, 0))
    WIN: input_events_received=3 kinds=[MouseMove { x: 400, y: 300 }, MouseMove { x: 363, y: 321 }, MouseMove { x: 400, y: 300 }]
    WIN: injected_moves=[(400, 300), (363, 321), (400, 300)]
    WIN: cursor_after=Some((399, 299))
    WIN: moved_to_target=true (target=Some((400, 300)) actual=Some((399, 299)))
    WIN: RESULT frames_received=5 frames_sent=8 input_events=3 moved_to_target=true elapsed_ms=28302
    WIN: VERDICT PASS

### 3.3 主控端原始输出（Mac）

    MAC: gateway=http://127.0.0.1:18090 relay=127.0.0.1:19000 device_code=368035388 captcha=894718
    MAC: session=ses-7d8574b1323296a0
    MAC: relay_registered local=127.0.0.1:54938
    MAC: handshake_verified (ed25519 + x25519)
    MAC: capture_ok 1920x1080
    MAC: frames_sent=5 bytes_total=88146
    MAC: frames_from_target=8 resolution=2880x1620 first_h264_bytes=9124
    MAC: input_events_sent=3 target=(400,300)
    MAC: DONE session=ses-7d8574b1323296a0

对得上：

- 主控发 5 帧 1920×1080 → 被控端 frames_received=5 resolution=1920x1080
- 被控收 8 帧 2880×1620 → 主控 frames_from_target=8 resolution=2880x1620
- 主控发 3 个鼠标移动 → 被控注入 3 个 → 光标从 (902,0) 移到 **(399,299)**（目标 400,300，误差 ≤2px）

---

## 四、复现（照抄即可）

### 4.1 Windows：依赖 + 构建 + 测试

    # 主机侧
    scp /tmp/er-core.tgz win:C:/EasyRemote-core.tgz
    ssh win 'powershell -NoProfile -Command "tar -xzf C:\EasyRemote-core.tgz -C C:\EasyRemote"'
    ssh win 'cmd /c "C:\EasyRemote\mp.cmd cmd /c "cd /d C:\EasyRemote\core && cargo build --workspace""'
    ssh win 'cmd /c "C:\EasyRemote\mp.cmd cmd /c "cd /d C:\EasyRemote\core && cargo test --workspace""'

mp.cmd 内容（必须有，否则 link.exe 找不到）：

    @echo off
    call "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul 2>&1
    %*

### 4.2 NVENC 验证

    # 交互会话里跑（Session 0 抓不到真屏幕，会退化成合成帧）
    schtasks /create /tn ERBench /tr "C:\EasyRemote\run_bench.cmd" /sc once /st 00:00 /ru Administrator /it /f
    schtasks /run /tn ERBench
    # run_bench.cmd: cd /d C:\EasyRemote\core && .\target\debug\examples\nvenc_bench.exe 1920 1080 30 > C:\EasyRemote\bench.log 2>&1

### 4.3 跨机会话

    # ① Mac：真实服务（gateway + rendezvous 在 stack，relay 独立）
    go build -o /tmp/erbin/stack  ./cmd/stack
    go build -o /tmp/erbin/relay  ./cmd/relay
    go build -o /tmp/erbin/udptun ./cmd/udptun
    GOOS=windows GOARCH=amd64 go build -o /tmp/erbin/udptun.exe ./cmd/udptun
    /tmp/erbin/stack  -addr 127.0.0.1:18090 &
    /tmp/erbin/relay  -addr 127.0.0.1:19000 &
    /tmp/erbin/udptun -mode server -listen 127.0.0.1:19001 -target 127.0.0.1:19000 &

    # ② Mac：把两个 TCP 入口反向映射到 Windows
    ssh -N -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win &

    # ③ Windows：UDP 隧道客户端（127.0.0.1:19000 的 UDP 走到 Mac 的中继）
    udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001

    # ④ Windows：交互会话跑被控端（写出 hello 文件）
    schtasks /create /tn ERTarget /tr "C:\EasyRemote\run_target.cmd" /sc once /st 00:00 /ru Administrator /it /f
    schtasks /run /tn ERTarget

    # ⑤ Mac：取走 hello，跑主控
    scp win:C:/EasyRemote/win_hello.txt /tmp/win_hello.txt
    ./target/debug/examples/xmachine_controller http://127.0.0.1:18090 127.0.0.1:19000 13700005202 123456 /tmp/win_hello.txt 5 8 400 300

---

## 五、明确未完成 / 仍有水分（不粉饰）

| 项 | 状态 | 说明 |
|---|---|---|
| NVENC 零拷贝 | ❌ 未做 | 每帧仍用 cuMemcpy2D 把 BGRA 从 host 拷进显存（2880×1620 约 18MB/帧）。真零拷贝要用 DXGI Desktop Duplication 的 ID3D11Texture2D 直接 nvEncRegisterResource（D3D11 设备类型） |
| 采集瓶颈 | ❌ 未换 | 仍是 xcap 全帧拷贝，2880×1620 一帧采集约 600ms 量级；编码已 7.8ms，采集成了端到端主瓶颈 |
| NVENC 覆盖面 | 🟡 部分 | 只有 H.264 + ARGB 输入；没有 HEVC/AV1，没有 NV12 路径，没有异步编码（enableEncodeAsync 仍为 0） |
| SPS/PPS 完整性 | 🟡 部分 | repeatSPSPPS=1 已设，但只验证到「解码成功且尺寸一致」，没做 NAL 级解析断言 |
| 中继 UDP 直连 | 🟡 部分 | Windows→Mac 的 UDP 走 udptun（UDP over TCP），会引入 TCP 队头阻塞；不是原生 UDP 直连 |
| 会话重连（跨机） | ❌ 未做 | 本轮没测跨机断线续传；SSH 隧道断了要重来（udptun 会为下一条 TCP 重连，会话本身不续） |
| ICE / NAT 打洞 | ❌ 未做 | 与上一轮一致 |
| Windows 部署形态 | 🟡 部分 | 仍靠 schtasks /IT 手工放进交互会话；生产要「服务 + 会话内辅助进程」双组件，本轮只验证了后者 |
| Flutter/UI 接硬编 | ❌ 未做 | er-ffi 走的是 OpenH264Codec 直连，没改用 create_encoder，UI 侧拿不到硬件编码 |

---

## 六、下一轮建议（按性价比）

1. **DXGI Desktop Duplication 采集 + D3D11 直通 NVENC**：把采集与上传（现在约 600ms）压到约 10ms，
   这是 40ms 预算唯一现实的路径。
2. er-ffi 与 UI 切到 create_encoder，让 App 也用上硬编。
3. 跨机链路换原生 UDP 中继（或在公网 VPS 上跑 relay），去掉 udptun 的 TCP 隧道。
4. 弱网/丢包矩阵（Mac 侧中继出口上做 tc netem）。
