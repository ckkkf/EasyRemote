//! 跨机验证 · 主控端（Windows）。
//!
//! 与 xmachine_controller（Mac 主控）镜像：读被控端（macOS）写出的 device_code/captcha →
//! 登录 → 绑设备 → 发起连接 → 握手 → 发帧 → **发输入** → 收被控端真实屏幕帧并解码。
//!
//! 为什么主控端不需要交互桌面：主控只"发"输入、"收"画面，不注入本机、不读本机光标。
//! 因此可以在 SSH 会话（Session 0）里跑——这也让 step4 少一个 schtasks 环节。
//! 代价是 Session 0 里的 DXGI 抓不到真实桌面：采集失败会自动退化为合成帧（真实编码 →
//! 加密 → 中继 → 解密 → 解码链路不变），日志会明确写 capture_unavailable。
//!
//! 用法：
//!   win_controller <gateway> <relay_udp> <phone> <sms> <hello_file> <out_frames> <in_frames> [move_x move_y]

#[cfg(target_os = "windows")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "windows"))]
fn main() {
    println!("win_controller 只能在 Windows 上跑（主控端采集需要 DXGI/GDI）");
}

#[cfg(target_os = "windows")]
fn real_main() {
    use std::net::SocketAddr;
    use std::time::{Duration, Instant};

    use er_capture::{Capture, ScreenCapture};
    use er_client::agent::{encode_video_msg, ControllerAgent, FrameInfo, MSG_VIDEO};
    use er_client::link::VideoHeader;
    use er_codec::{create_encoder, EncodeConfig, Encoder};
    use er_input::InputEvent;
    use er_session::crypto::CryptoSession;
    use er_session::relay_client::RelayClient;

    fn arg(i: usize, default: &str) -> String {
        std::env::args()
            .nth(i)
            .unwrap_or_else(|| default.to_string())
    }

    /// 合成渐变帧 → H.264 → 加密 → 中继。
    /// 用于 Session 0（ssh）拿不到真实桌面的场景；链路与真实采集完全一致。
    fn send_synthetic(
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
    ) -> Vec<usize> {
        let (w, h) = (320u32, 240u32);
        let cfg = EncodeConfig {
            width: w,
            height: h,
            fps: 60,
            bitrate_kbps: 1000,
            gop: 60,
            bframes: 0,
        };
        let mut codec = create_encoder(cfg).expect("codec");
        let mut frame = vec![0u8; (w * h * 4) as usize];
        let mut sizes = Vec::with_capacity(count);
        for i in 0..count {
            for y in 0..h {
                for x in 0..w {
                    let idx = ((y * w + x) * 4) as usize;
                    frame[idx] = ((x + i as u32) % 256) as u8;
                    frame[idx + 1] = (y % 256) as u8;
                    frame[idx + 2] = 128;
                    frame[idx + 3] = 255;
                }
            }
            let encoded = codec.encode((i as u64) * 16, &frame).expect("encode");
            if encoded.data.is_empty() {
                continue;
            }
            let hdr = VideoHeader {
                seq: i as u32,
                ts_ms: (i as u64 * 16) as u32,
                width: w as u16,
                height: h as u16,
            };
            let msg = encode_video_msg(&hdr, &encoded.data);
            let sealed = crypto.seal(&msg);
            relay.send(&sealed).expect("relay send");
            sizes.push(encoded.data.len());
            std::thread::sleep(Duration::from_millis(16));
        }
        sizes
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700008102");
    let sms = arg(4, "123456");
    let hello_file = arg(5, "C:\\EasyRemote\\mac_hello.txt");
    let out_frames: usize = arg(6, "5").parse().expect("out_frames");
    let in_frames: usize = arg(7, "5").parse().expect("in_frames");
    let move_x: i32 = arg(8, "400").parse().expect("move_x");
    let move_y: i32 = arg(9, "300").parse().expect("move_y");
    // 演示模式：demo_secs > 0 时持续画圆发鼠标移动（被控端实时注入，肉眼能看到光标动）
    let demo_secs: u64 = arg(10, "0").parse().unwrap_or(0);
    // 录像：非空则把收到的 H.264 裸码流（Annex-B）写到该文件，Mac 侧用 ffmpeg 转 mp4
    let record_file = arg(11, "");

    let t_start = Instant::now();
    println!(
        "WIN: gateway={base} relay={relay_addr} pid={}",
        std::process::id()
    );

    // 设备码/验证码由被控端写出（真实产品里显示在被控端屏幕上）
    let deadline = Instant::now() + Duration::from_secs(180);
    let hello = loop {
        if let Ok(s) = std::fs::read_to_string(&hello_file) {
            if s.contains("captcha=") {
                break s;
            }
        }
        if Instant::now() > deadline {
            panic!("hello file never appeared: {hello_file}");
        }
        std::thread::sleep(Duration::from_millis(300));
    };
    let field = |key: &str| -> String {
        for line in hello.lines() {
            if let Some(rest) = line.strip_prefix(key) {
                return rest.trim().to_string();
            }
        }
        panic!("missing {key}");
    };
    let code = field("device_code=");
    let captcha = field("captcha=");
    println!("WIN: device_code={code} captcha={captcha}");

    let ctrl = ControllerAgent::start(&base, &phone, &sms, relay_addr).expect("ctrl start");
    let session_id = ctrl
        .request_session(&code, &captcha)
        .expect("request session");
    println!("WIN: session={session_id}");
    // 等被控端 consent 落地（真实产品里用户点「接受」也要时间），否则取票据会被拒
    std::thread::sleep(Duration::from_millis(1500));

    let (mut relay, mut crypto, keys, msg) = ctrl.begin(&session_id).expect("ctrl begin");
    println!("WIN: relay_registered local={}", relay.local_addr());
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay, &msg).expect("announce");
    ctrl.finish(&mut relay, &mut crypto, &keys, &session_id)
        .expect("handshake");
    println!("WIN: handshake_verified (ed25519 + x25519)");

    // ---------- ① 发给被控端视频帧 ----------
    //
    // Session 0（SSH 会话）里 DXGI 能 open 但抓不到帧：open_primary 返回 1024x768 假桌面，
    // capture_and_encode 随后报 capture unavailable。这时不能 panic——主控端本来只负责
    // 发输入/收画面，画面用合成帧即可（编码→加密→中继→解码链路与真实采集完全一致）。
    let mut frames_sent: Vec<usize> = Vec::new();
    let mut frames_path = "capture";
    match ScreenCapture::open_primary() {
        Ok(mut cap) => {
            let info = cap.info();
            println!("WIN: capture_ok {}x{}", info.width, info.height);
            match ctrl.send_frames(&mut relay, &mut crypto, out_frames) {
                Ok(sizes) if !sizes.is_empty() => frames_sent = sizes,
                Ok(_) => {
                    println!("WIN: capture_produced_no_frames; fallback=synthetic_h264");
                    frames_path = "synthetic";
                }
                Err(e) => {
                    println!("WIN: capture_send_failed ({e}); fallback=synthetic_h264");
                    frames_path = "synthetic";
                }
            }
        }
        Err(e) => {
            println!("WIN: capture_unavailable ({e}); fallback=synthetic_h264");
            frames_path = "synthetic";
        }
    }
    if frames_path == "synthetic" {
        frames_sent = send_synthetic(&mut relay, &mut crypto, out_frames);
    }
    println!(
        "WIN: frames_sent={} bytes_total={} path={frames_path}",
        frames_sent.len(),
        frames_sent.iter().sum::<usize>()
    );

    // ---------- ② 发输入 ----------
    // 演示模式要等被控端从"收帧"切到"收输入"：它刚收完 5 帧，还要读一次光标基线；
    // 立刻灌输入会打进它的 UDP 缓冲（丢包），屏幕上只会看到光标跳一下。
    let n: usize = if demo_secs > 0 {
        std::thread::sleep(Duration::from_millis(3000));
        // 画圆：被控端实时注入，肉眼可见鼠标在 macOS 桌面上转圈
        let total = (demo_secs * 25) as usize;
        let (cx, cy, r) = (960.0f64, 540.0f64, 220.0f64);
        let mut sent = 0usize;
        for i in 0..total {
            let t = i as f64 / total as f64 * std::f64::consts::TAU;
            let x = (cx + r * t.cos()).round() as i32;
            let y = (cy + r * t.sin()).round() as i32;
            sent += ctrl
                .send_input(&mut relay, &mut crypto, &[InputEvent::MouseMove { x, y }])
                .expect("send demo move");
            std::thread::sleep(Duration::from_millis(40));
        }
        println!("WIN: demo_moves_sent={sent} circle_center=({cx},{cy}) r={r} secs={demo_secs}");
        sent
    } else {
        let events = vec![
            InputEvent::MouseMove {
                x: move_x,
                y: move_y,
            },
            InputEvent::MouseMove {
                x: move_x - 37,
                y: move_y + 21,
            },
            InputEvent::MouseMove {
                x: move_x,
                y: move_y,
            },
        ];
        let sent = ctrl
            .send_input(&mut relay, &mut crypto, &events)
            .expect("send input");
        println!("WIN: input_events_sent={sent} target=({move_x},{move_y}) sent_before_recv=true");
        sent
    };

    // ---------- ③ 收被控端（macOS）真实屏幕帧 ----------
    // 收不到帧时不要 panic：打印 0 帧并让 VERDICT 判 FAIL，日志才完整可断言。
    // record_file 非空 = 录像模式：只落 H.264 码流不解码（软解 1080p 太慢会撑爆接收缓冲）。
    let got: Vec<FrameInfo> = if record_file.is_empty() {
        ctrl.recv_frames(&mut relay, &mut crypto, in_frames)
            .unwrap_or_default()
    } else {
        let frames = ctrl
            .recv_video_raw(&mut relay, &mut crypto, in_frames, Duration::from_secs(90))
            .unwrap_or_default();
        let mut all: Vec<u8> = Vec::new();
        for f in &frames {
            all.extend_from_slice(f);
        }
        if !all.is_empty() {
            std::fs::write(&record_file, &all).expect("write record file");
            println!(
                "WIN: recorded_frames={} bytes={} path={record_file}",
                frames.len(),
                all.len()
            );
        }
        frames
            .iter()
            .map(|f| FrameInfo { width: 0, height: 0, h264_bytes: f.len() })
            .collect()
    };
    if got.is_empty() {
        println!("WIN: frames_from_target=0 (mac 侧没有发出可解码的帧)");
    } else if record_file.is_empty() {
        println!(
            "WIN: frames_from_target={} resolution={}x{} first_h264_bytes={}",
            got.len(),
            got[0].width,
            got[0].height,
            got[0].h264_bytes
        );
    } else {
        println!("WIN: frames_from_target={} (录像模式，码流已落盘)", got.len());
    }
    println!("WIN: MSG_VIDEO={MSG_VIDEO} (帧头 seq/ts/dim 已随帧传输)");
    println!(
        "WIN: RESULT frames_sent_target={} frames_from_target={} input_events_sent={} frames_path={frames_path} elapsed_ms={}",
        frames_sent.len(),
        got.len(),
        n,
        t_start.elapsed().as_millis()
    );
    println!("WIN: DONE session={session_id}");

    if got.is_empty() || n == 0 {
        println!("WIN: VERDICT FAIL");
        std::process::exit(1);
    }
    println!("WIN: VERDICT PASS");
}
