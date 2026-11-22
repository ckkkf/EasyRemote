//! macOS 被控端（**持续会话**）：把主控发来的输入真注入，同时持续采集本机屏幕回传。
//!
//! 与 mac_target 的区别：mac_target 是一次性验证（收 5 帧→发 5 帧→退出），
//! 这里是给 GUI 主控端用的长连接——主控连着多久就共享多久，直到 duration 到期或
//! 十分钟没有输入（对端大概已经走了）。
//!
//! 用法：
//!   mac_host <gateway> <relay_udp> <phone> <sms> <signal_base> <hello_file> [fps] [duration_secs]
//!
//! duration_secs = 0 表示一直跑到 Ctrl-C / 主控断开。

#[cfg(target_os = "macos")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "macos"))]
fn main() {
    println!("mac_host 只能在 macOS 上跑（被控端注入需要 CGEventPost）");
}

#[cfg(target_os = "macos")]
fn real_main() {
    use std::net::SocketAddr;
    use std::time::{Duration, Instant};

    use er_capture::{Capture, ScreenCapture};
    use er_client::agent::{encode_video_msg, TargetAgent, MSG_HEARTBEAT, MSG_INPUT};
    use er_client::encoder::HybridEncoder;
    use er_client::link::VideoHeader;
    use er_codec::EncodeConfig;
    use er_input::macos::{MacDisplay, MacInjector};
    use er_input::{Injector, InputEvent};

    fn arg(i: usize, default: &str) -> String {
        std::env::args()
            .nth(i)
            .unwrap_or_else(|| default.to_string())
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700009101");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let hello_file = arg(6, "mac_hello.txt");
    let fps: u64 = arg(7, "30").parse().unwrap_or(30);
    let duration_secs: u64 = arg(8, "0").parse().unwrap_or(0);
    let fps = fps.clamp(5, 60);

    let t_start = Instant::now();
    let disp = MacDisplay::main();
    println!(
        "MAC: host gateway={base} relay={relay_addr} display={}x{} scale={} fps={fps} duration={duration_secs}",
        disp.pixel_width,
        disp.pixel_height,
        disp.scale()
    );

    // 采集能力自检：屏录权限缺失时这里就报出来，而不是让主控端黑屏
    match ScreenCapture::open_primary() {
        Ok(mut cap) => match cap.grab() {
            Ok(f) => println!(
                "MAC: capture_ok {}x{} backend={}",
                f.width,
                f.height,
                cap.backend_name()
            ),
            Err(e) => println!("MAC: capture_error={e}"),
        },
        Err(e) => println!("MAC: capture_unavailable={e}"),
    }
    let acc = MacInjector::cursor_pixel();
    println!("MAC: cursor_baseline_pixel={acc:?}");

    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    std::fs::write(
        &hello_file,
        format!(
            "device_id={}\ndevice_code={}\ncaptcha={}\n",
            target.device_id(),
            code,
            captcha
        ),
    )
    .expect("write hello");
    // 「临时密码」：被控端本地生成并保存在 ~/.easyremote/host_password，
    // 长期有效、校验不消费；服务端只存 HMAC。面板显示它（可看明文/可复制）。
    let password = ensure_host_password();
    match target.set_password(&password) {
        Ok(()) => println!("MAC: host_password={password} (saved to ~/.easyremote/host_password)"),
        Err(e) => println!("MAC: set_password_failed={e}"),
    }
    write_panel(&code, &password);
    println!(
        "MAC: device_id={} device_code={} captcha={}",
        target.device_id(),
        code,
        captcha
    );
    // 被控端要能**反复被连**：一个会话结束后回到等待，而不是进程直接收工。
    // 之前是单次结构，第一个主控走后（哪怕是测试）后面所有连接都石沉大海
    // —— 表现就是主控卡在「等待对方授权」，而被控端日志一片安静。
    loop {
        println!("MAC: waiting_for_signalling ...");

        let session_id = target
            .wait_for_session(&signal, Duration::from_secs(3600))
            .expect("signalling");
        println!("MAC: session={session_id}");
        if let Err(e) = target.consent(&session_id) {
            println!("MAC: consent_failed={e}");
            continue;
        }
        println!("MAC: consent=authorized");

        std::thread::sleep(Duration::from_millis(1200));
        let (mut relay, mut crypto, msg) = match target.begin(&session_id) {
            Ok(v) => v,
            Err(e) => {
                println!("MAC: begin_failed={e}");
                continue;
            }
        };
        println!("MAC: relay_registered local={}", relay.local_addr());
        std::thread::sleep(Duration::from_millis(2500));
        if let Err(e) = target.announce(&mut relay, &msg) {
            println!("MAC: announce_failed={e}");
            continue;
        }
        // 握手失败（主控没来/走了）不该让整个被控端崩掉：回到等待接下一个
        if let Err(e) = target.finish(&mut relay, &mut crypto, &session_id) {
            println!("MAC: handshake_failed={e}");
            continue;
        }
        println!("MAC: handshake_verified (ed25519 + x25519)");
        println!("MAC: streaming ...");

        let mut cap = ScreenCapture::open_primary().expect("capture");
        let info = cap.info();
        let cfg = EncodeConfig {
            width: info.width & !1,
            height: info.height & !1,
            fps: fps as u32,
            bitrate_kbps: 4000,
            // is_low_latency() 要求 gop <= fps；写死 60 会在 30fps 下被拒（实测 unsupported config）
            gop: fps as u32,
            bframes: 0,
        };
        let mut enc = HybridEncoder::new(cfg).expect("encoder");
        let mut injector = MacInjector::new(true);

        let frame_budget = Duration::from_millis((1000 / fps).max(1));
        let mut seq: u32 = 0;
        let mut sent: u64 = 0;
        let mut injected: u64 = 0;
        let mut last_input = Instant::now();
        let mut last_beat = Instant::now();
        let mut last_stat = Instant::now();
        let mut last_rotate = Instant::now();

        while duration_secs == 0 || t_start.elapsed().as_secs() < duration_secs {
            // 主控断开判据：只在「没显式给时长」时启用（无人值守的默认模式）。
            // 显式给了 duration 就按 duration 走——演示时用户可能只是盯着画面不动鼠标，
            // 按静默超时退出会把正在看的会话掐断（实测 10 分钟就断了）。
            // 心跳超时优先：主控走了就别继续空推（CLI 端与客户端行为保持一致）
            if last_beat.elapsed() > Duration::from_secs(10) {
                println!("MAC: peer_left (no heartbeat)");
                break;
            }
            if duration_secs == 0 && last_input.elapsed() > Duration::from_secs(1800) {
                println!("MAC: no input for 1800s, assuming controller left");
                break;
            }

            // ① 先把积压的输入事件收干净（短超时，最多 32 条/轮，避免输入饿死采集）
            let mut drained = 0;
            while drained < 32 {
                match relay.recv_timeout(Duration::from_millis(2)) {
                    Ok(Some(sealed)) => {
                        if let Ok(plain) = crypto.open(&sealed) {
                            if !plain.is_empty() && plain[0] == MSG_HEARTBEAT {
                                last_beat = Instant::now();
                            }
                            if !plain.is_empty() && plain[0] == MSG_INPUT {
                                if let Some(ev) = InputEvent::decode(&plain[1..]) {
                                    if injector.inject(&ev).is_ok() {
                                        injected += 1;
                                    }
                                    last_input = Instant::now();
                                }
                            }
                        }
                    }
                    Ok(None) => break,
                    Err(_) => break,
                }
                drained += 1;
            }

            // ② 采集一帧 → 编码 → 加密 → 中继
            let t0 = Instant::now();
            if let Ok(Some(encoded)) = enc.capture_and_encode(&mut cap) {
                if !encoded.is_empty() {
                    seq = seq.wrapping_add(1);
                    let hdr = VideoHeader {
                        seq,
                        ts_ms: t_start.elapsed().as_millis() as u32,
                        width: (cfg.width & 0xffff) as u16,
                        height: (cfg.height & 0xffff) as u16,
                    };
                    let msg = encode_video_msg(&hdr, &encoded);
                    let sealed = crypto.seal(&msg);
                    if relay.send(&sealed).is_ok() {
                        sent += 1;
                    }
                }
            }

            // ③ 按目标帧率节流（编码耗时已占一部分预算）
            let spent = t0.elapsed();
            if spent < frame_budget {
                std::thread::sleep(frame_budget - spent);
            }

            // 验证码自动轮换：服务端 TTL 10 分钟且一次即失效，长期挂着的被控端如果不换，
            // 5 分钟后就只能重启进程才能拿到新码（实测用户每次都要等我重启）。
            // 这里每 5 分钟 rotate 一次并打印，日志里永远有一条新的可用验证码。
            if last_rotate.elapsed() >= Duration::from_secs(300) {
                // 一次性验证码仍然轮换（兼容旧客户端与「验证码」入口），但面板显示的是长期密码。
                match target.rotate_captcha() {
                    Ok(code) => println!("MAC: captcha_refreshed={code}"),
                    Err(e) => println!("MAC: captcha_refresh_failed={e}"),
                }
                last_rotate = Instant::now();
            }

            if last_stat.elapsed() >= Duration::from_secs(5) {
                println!(
                    "MAC: stats sent={sent} injected={injected} backend={} elapsed={}s",
                    enc.backend(),
                    t_start.elapsed().as_secs()
                );
                last_stat = Instant::now();
            }
        }

        println!("MAC: session_closed sent={sent} injected={injected}, back to waiting",);
        if duration_secs > 0 && t_start.elapsed().as_secs() >= duration_secs {
            break;
        }
    }

    println!("MAC: HOST_DONE elapsed={}s", t_start.elapsed().as_secs());
}

/// 被控端「临时密码」：本地生成一次、长期复用，明文存 ~/.easyremote/host_password。
///
/// 与一次性验证码的区别：不轮换、不消费，用户可以在被控端界面"查看明文 / 复制"，
/// 主控端输入「设备码 + 这个密码」即可连接，不用每 10 分钟去找新码。
#[cfg(target_os = "macos")]
fn ensure_host_password() -> String {
    let home = std::env::var("HOME").unwrap_or_else(|_| ".".to_string());
    let dir = format!("{home}/.easyremote");
    let path = format!("{dir}/host_password");
    if let Ok(s) = std::fs::read_to_string(&path) {
        let s = s.trim().to_string();
        if s.len() >= 6 {
            return s;
        }
    }
    // 8 位数字：方便口述/手输，同时避免 0/O、1/l 这类歧义字符
    let mut seed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(12345);
    seed ^= (std::process::id() as u64).wrapping_mul(0x9E37_79B9_7F4A_7C15);
    let mut pw = String::with_capacity(8);
    for _ in 0..8 {
        seed = seed
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        pw.push(char::from(b'0' + ((seed >> 33) % 10) as u8));
    }
    let _ = std::fs::create_dir_all(&dir);
    let _ = std::fs::write(&path, &pw);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600));
    }
    pw
}

/// 把「设备码 / 临时密码」写到固定文件，供被控端 GUI 显示（密码长期有效，无需时间戳）。
#[cfg(target_os = "macos")]
fn write_panel(device_code: &str, password: &str) {
    let ts = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let body = format!("{device_code}\n{password}\n{ts}\n");
    let _ = std::fs::write("/tmp/er_captcha.txt", body);
}
