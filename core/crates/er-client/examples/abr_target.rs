//! ABR 数据面 · 被控端（Windows，视频发送侧 + 编码器重配）。
//!
//! 闭环：
//!   采集 → 编码（`HybridEncoder`，零拷贝优先）→ 带 seq 的视频帧 → 中继
//!   ← 反向通道收 `MSG_FEEDBACK` → `AbrDriver::on_report` → `AbrController` 决策
//!   → `QualityLevel` → `EncodeConfig`（码率/分辨率/帧率）→ `HybridEncoder::reconfigure`
//!
//! 必须在交互会话里跑（DXGI 采集需要），用 schtasks /IT 启动。
//!
//! 用法：
//!   abr_target.exe <gateway> <relay_udp> <phone> <sms> <signal_base> <hello_file> \
//!                  <seconds> <scene_secs> <frames_per_sec>
//!
//! 输出（每条都是可 grep 的原始证据）：
//!   ABR_TX: level=.. bitrate=.. dims=.. backend=..      —— 每档生效时打一次
//!   ABR_TX_CYCLE: t=.. fb=.. loss=..% rtt=..ms ...        —— 每份反馈打一次
//!   ABR_TX_SUMMARY: ...                                  —— 收尾

#[cfg(target_os = "windows")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "windows"))]
fn main() {
    println!("abr_target 只能在 Windows 上跑（DXGI + NVENC 采集编码）");
}

#[cfg(target_os = "windows")]
fn real_main() {
    use std::net::SocketAddr;
    use std::time::{Duration, Instant};

    use er_capture::{Capture, ScreenCapture};
    use er_client::agent::{now_ms, TargetAgent, MSG_FEEDBACK};
    use er_client::encoder::HybridEncoder;
    use er_client::link::{encode_config_for, AbrDriver, FeedbackReport, VideoHeader};

    fn arg(i: usize, default: &str) -> String {
        std::env::args()
            .nth(i)
            .unwrap_or_else(|| default.to_string())
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19001").parse().expect("relay addr");
    let phone = arg(3, "13700007201");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let hello_file = arg(6, "win_hello.txt");
    let seconds: u64 = arg(7, "25").parse().expect("seconds");
    let scene_secs: u32 = arg(8, "40").parse().expect("scene_secs");
    let fps_cap: u32 = arg(9, "30").parse().expect("frames_per_sec");

    println!("ABR_TX_BOOT: gateway={base} relay={relay_addr} signal={signal} seconds={seconds}");

    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let device_code = target.device_code().to_string();
    let captcha = target.rotate_captcha().expect("captcha");
    println!(
        "ABR_TX_BOOT: device_id={} device_code={device_code} captcha={captcha}",
        target.device_id()
    );
    std::fs::write(
        &hello_file,
        format!(
            "device_id={}\ndevice_code={device_code}\ncaptcha={captcha}\n",
            target.device_id()
        ),
    )
    .expect("write hello");

    let session_id = target
        .wait_for_session(&signal, Duration::from_secs(180))
        .expect("signalling");
    target.consent(&session_id).expect("consent");
    println!("ABR_TX_BOOT: session={session_id} consent=authorized");
    std::thread::sleep(Duration::from_millis(1200));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).expect("begin");
    target.announce(&mut relay, &msg).expect("announce");
    target
        .finish(&mut relay, &mut crypto, &session_id)
        .expect("handshake");
    println!("ABR_TX_BOOT: handshake_verified");

    // 源尺寸决定各档分辨率
    let mut cap = ScreenCapture::open_primary().expect("no display");
    let info = cap.info();
    let (src_w, src_h) = (info.width & !1, info.height & !1);
    println!("ABR_TX_BOOT: capture_display={src_w}x{src_h}");

    // 画面必须有变化，否则 DXGI 不出帧，ABR 没有数据可调
    let scene = if scene_secs > 0 {
        let p = std::process::Command::new("powershell.exe")
            .args([
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                "C:\\EasyRemote\\scene_move.ps1",
                "-Seconds",
                &scene_secs.to_string(),
                "-StepMs",
                "40",
            ])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .ok();
        println!("ABR_TX_BOOT: scene=on pid={:?}", p.as_ref().map(|c| c.id()));
        p
    } else {
        println!("ABR_TX_BOOT: scene=off");
        None
    };

    let mut driver = AbrDriver::new(src_w, src_h);
    let mut cfg = driver.config();
    let mut enc = HybridEncoder::new(cfg).expect("encoder");
    let _ = cap.prime_frame();
    println!(
        "ABR_TX: level={} bitrate_kbps={} fps={} dims={}x{} backend={} reason=initial",
        driver.level().name,
        cfg.bitrate_kbps,
        cfg.fps,
        cfg.width,
        cfg.height,
        enc.backend()
    );

    let mut seq: u32 = 0;
    let mut sent = 0usize;
    let mut bytes = 0usize;
    let mut fbs = 0u32;
    let mut rtt_min = u32::MAX;
    let mut rtt_max = 0u32;
    let mut last_level = driver.level();
    let mut last_backend = enc.backend().to_string();
    let mut frames_without_change = 0u32;
    let start = Instant::now();
    let deadline = start + Duration::from_secs(seconds);
    let mut next_frame = Instant::now();

    while Instant::now() < deadline {
        // ① 采集 + 编码 + 发送（按 fps 档位节流）
        let frame_interval = Duration::from_millis(1000 / cfg.fps.max(1) as u64);
        if Instant::now() >= next_frame {
            next_frame = Instant::now() + frame_interval;
            match enc.capture_and_encode(&mut cap) {
                Ok(Some(data)) if !data.is_empty() => {
                    seq = seq.wrapping_add(1);
                    let hdr = VideoHeader {
                        seq,
                        ts_ms: now_ms() as u32,
                        width: cfg.width as u16,
                        height: cfg.height as u16,
                    };
                    let msg = er_client::agent::encode_video_msg(&hdr, &data);
                    let sealed = crypto.seal(&msg);
                    if relay.send(&sealed).is_ok() {
                        sent += 1;
                        bytes += data.len();
                        frames_without_change = 0;
                    }
                }
                Ok(_) => {
                    frames_without_change += 1;
                }
                Err(e) => println!("ABR_TX_ERR: encode: {e}"),
            }
        }

        // ② 反向通道：收反馈（非阻塞，1ms）
        if let Ok(Some(sealed)) = relay.recv_timeout(Duration::from_millis(1)) {
            let plain = match crypto.open(&sealed) {
                Ok(p) => p,
                Err(_) => continue,
            };
            if plain.is_empty() || plain[0] != MSG_FEEDBACK {
                continue;
            }
            let Some(report) = FeedbackReport::decode(&plain[1..]) else {
                continue;
            };
            // RTT：报告里回显的是我们发帧时刻，用本机钟相减，两端不需要对齐
            // 不要用 now_ms() - echo_ts_ms 算 RTT：两者在不同时钟域
            // （echo_ts 是接收侧的到达时间戳，now_ms 是发送侧单调时钟），
            // 相减得到的是"时钟差 + 真实时延"，实测 67..752ms 乱跳，
            // 会让 ABR 在 0% 丢包时也判成严重拥塞。
            // 真实 RTT 需要一次探测帧往返（当前未实现），这里先用接收侧的 hint，
            // 未提供时按 0 处理，即"不按 RTT 降档"。
            let rtt_ms = report.rtt_hint_ms;
            fbs += 1;
            rtt_min = rtt_min.min(rtt_ms);
            rtt_max = rtt_max.max(rtt_ms);

            println!(
                "ABR_TX_CYCLE: t={:.1}s fb_seq={} cycle_expected={} cycle_received={} frame_loss={:.1}% frag_recv={} frag_missing={} datagram_loss={:.1}% rtt={}ms queue={}ms",
                start.elapsed().as_secs_f64(),
                report.seq,
                report.cycle_expected,
                report.cycle_received,
                report.loss_pct,
                report.frag_received,
                report.frag_missing,
                report.datagram_loss_pct,
                rtt_ms,
                report.queue_ms
            );

            let (idx, healthy, severe, mild) = driver.debug_state();
            if fbs % 5 == 0 {
                println!("ABR_TX_STATE fb={} idx={} healthy={} severe_s={} mild_s={} loss={:.1}% q={} rtt={}", fbs, idx, healthy, severe, mild, report.loss_pct, report.queue_ms, rtt_ms);
            }
            if let Some((decision, next_cfg)) = driver.on_report(&report, rtt_ms) {
                let prev = last_level;
                if let Err(e) = enc.reconfigure(next_cfg) {
                    println!("ABR_TX_ERR: reconfigure: {e}");
                } else {
                    cfg = next_cfg;
                    last_level = decision.level;
                    frames_without_change = 0;
                    println!(
                        "ABR_TX: level={}->{} bitrate_kbps={}->{} fps={}->{} dims={}x{} backend={} changed=true reason={}",
                        prev.name,
                        decision.level.name,
                        encode_config_for(&prev, src_w, src_h).bitrate_kbps,
                        cfg.bitrate_kbps,
                        encode_config_for(&prev, src_w, src_h).fps,
                        cfg.fps,
                        cfg.width,
                        cfg.height,
                        enc.backend(),
                        decision.reason
                    );
                }
            }
            // backend 变化（例如降分辨率后离开零拷贝）单独打一行
            let b = enc.backend().to_string();
            if b != last_backend {
                println!("ABR_TX: backend={b}（前 {last_backend}）");
                last_backend = b;
            }
        }
    }

    if let Some(mut c) = scene {
        let _ = c.kill();
        let _ = c.wait();
    }

    println!(
        "ABR_TX_SUMMARY: frames_sent={sent} bytes_total={bytes} feedbacks={} changes={} final_level={} final_bitrate_kbps={} final_fps={} final_dims={}x{} backend={} rtt_min={}ms rtt_max={}ms idle_skips={frames_without_change} elapsed_ms={}",
        driver.feedbacks(),
        driver.changes(),
        driver.level().name,
        cfg.bitrate_kbps,
        cfg.fps,
        cfg.width,
        cfg.height,
        enc.backend(),
        if rtt_min == u32::MAX { 0 } else { rtt_min },
        rtt_max,
        start.elapsed().as_millis()
    );
    println!("ABR_TX_DONE session={session_id}");
}
