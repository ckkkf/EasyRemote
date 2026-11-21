//! 弱网矩阵 · 被控侧接收器（Windows，Session 0 即可跑，不做采集/注入）。
//!
//! 只做一件事：在真实 gateway+relay 上接收视频帧，统计
//!   - 应用层送达帧数 / 缺失帧（整帧丢失，说明分片被丢导致重组失败）
//!   - 每帧时延（用 PING/PONG 估计的时钟偏移校正）
//!   - 解码成功/失败
//!
//! 用法：
//!   weaknet_recv.exe <gateway> <relay_udp> <phone> <sms> <signal_base> <hello_file> <idle_timeout_s>

#[cfg(target_os = "windows")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "windows"))]
fn main() {
    real_main();
}

fn real_main() {
    use std::collections::BTreeSet;
    use std::net::SocketAddr;
    use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

    use er_client::agent::TargetAgent;
    use er_codec::H264Decoder;

    const MSG_VIDEO: u8 = 1;
    const MSG_PING: u8 = 0x10;
    const MSG_PONG: u8 = 0x11;
    const MSG_DONE: u8 = 0x12;

    fn arg(i: usize, default: &str) -> String {
        std::env::args().nth(i).unwrap_or_else(|| default.to_string())
    }
    fn now_ms() -> u64 {
        SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis() as u64).unwrap_or(0)
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700006201");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let hello_file = arg(6, "win_hello.txt");
    let idle_timeout: u64 = arg(7, "12").parse().expect("idle_timeout");

    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    std::fs::write(
        &hello_file,
        format!("device_id={}\ndevice_code={}\ncaptcha={}\n", target.device_id(), code, captcha),
    )
    .expect("write hello");
    println!("RECV: ready device_code={code} captcha={captcha}");

    let session_id = target.wait_for_session(&signal, Duration::from_secs(180)).expect("signalling");
    println!("RECV: session={session_id}");
    target.consent(&session_id).expect("consent");
    std::thread::sleep(Duration::from_millis(1200));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).expect("begin");
    // UDP 下 register 与 announce 可能乱序到达中继；等一拍更稳
    std::thread::sleep(Duration::from_millis(400));
    let mut hs_attempt = 0;
    loop {
        target.announce(&mut relay, &msg).expect("announce");
        match target.finish(&mut relay, &mut crypto, &session_id) {
            Ok(()) => break,
            Err(e) => {
                hs_attempt += 1;
                if hs_attempt >= 3 {
                    panic!("handshake failed after {hs_attempt} attempts: {e}");
                }
                println!("RECV: handshake_retry={hs_attempt} ({e})");
            }
        }
    }
    println!("RECV: handshake_verified");

    let mut dec = H264Decoder::new().expect("decoder");
    let mut got: BTreeSet<u32> = BTreeSet::new();
    let mut latencies: Vec<i64> = Vec::new();
    let mut decode_ok = 0usize;
    let mut decode_fail = 0usize;
    let mut bytes_total = 0usize;
    let mut done_sent: Option<u32> = None;
    let mut first_arrival: Option<u64> = None;
    let mut last_arrival: Option<u64> = None;
    let idle = Duration::from_secs(idle_timeout);
    let hard_deadline = Instant::now() + Duration::from_secs(180);

    loop {
        if Instant::now() > hard_deadline {
            println!("RECV: hard_deadline");
            break;
        }
        let sealed = match relay.recv_timeout(idle) {
            Ok(Some(s)) => s,
            Ok(None) => {
                println!("RECV: idle_timeout");
                break;
            }
            Err(e) => {
                println!("RECV: recv_error={e}");
                break;
            }
        };
        let plain = match crypto.open(&sealed) {
            Ok(p) => p,
            Err(_) => continue,
        };
        if plain.is_empty() {
            continue;
        }
        match plain[0] {
            MSG_PING => {
                if plain.len() >= 9 {
                    let mut pong = vec![MSG_PONG];
                    pong.extend_from_slice(&plain[1..9]); // echo t0
                    pong.extend_from_slice(&now_ms().to_le_bytes()); // t1
                    let sealed = crypto.seal(&pong);
                    let _ = relay.send(&sealed);
                }
            }
            MSG_VIDEO => {
                if plain.len() < 1 + 4 + 8 + 8 {
                    continue;
                }
                let idx = u32::from_le_bytes(plain[1..5].try_into().unwrap());
                let ts = u64::from_le_bytes(plain[5..13].try_into().unwrap());
                let offset = i64::from_le_bytes(plain[13..21].try_into().unwrap());
                let h264 = &plain[21..];
                let arrival = now_ms();
                let lat = arrival as i64 - ts as i64 - offset;
                latencies.push(lat);
                bytes_total += h264.len();
                if first_arrival.is_none() {
                    first_arrival = Some(arrival);
                }
                last_arrival = Some(arrival);
                match dec.decode(h264) {
                    Ok(Some(_)) => decode_ok += 1,
                    Ok(None) => decode_ok += 1,
                    Err(_) => decode_fail += 1,
                }
                got.insert(idx);
            }
            MSG_DONE => {
                if plain.len() >= 5 {
                    done_sent = Some(u32::from_le_bytes(plain[1..5].try_into().unwrap()));
                }
                break;
            }
            _ => {}
        }
    }

    // 缺失帧 = 序号空洞（整帧丢失：分片丢了，重组失败）
    let sent = done_sent.unwrap_or_else(|| got.iter().next_back().copied().unwrap_or(0) + 1);
    let mut missing: Vec<u32> = (0..sent).filter(|i| !got.contains(i)).collect();
    let missing_desc = if missing.is_empty() {
        "none".to_string()
    } else if missing.len() <= 20 {
        format!("{missing:?}")
    } else {
        format!("{} frames, first 20 = {:?}", missing.len(), &missing[..20])
    };
    missing.clear();

    latencies.sort_unstable();
    let (p50, p95, max) = if latencies.is_empty() {
        (0, 0, 0)
    } else {
        (
            latencies[latencies.len() / 2],
            latencies[(latencies.len() * 95 / 100).min(latencies.len() - 1)],
            *latencies.last().unwrap(),
        )
    };
    let span = match (first_arrival, last_arrival) {
        (Some(a), Some(b)) => b.saturating_sub(a),
        _ => 0,
    };
    let delivered = got.len();
    let lost = sent.saturating_sub(delivered as u32);

    println!("RECV: frames_sent_by_peer={sent}");
    println!("RECV: frames_delivered={delivered}");
    println!("RECV: frames_lost={lost}");
    println!("RECV: missing_index={missing_desc}");
    println!("RECV: decode_ok={decode_ok} decode_fail={decode_fail}");
    println!("RECV: latency_ms p50={p50} p95={p95} max={max}");
    println!("RECV: bytes_total={bytes_total} span_ms={span}");
    println!(
        "RECV: RESULT sent={sent} delivered={delivered} lost={lost} loss_ratio={:.3} span_ms={span}",
        if sent > 0 { lost as f64 / sent as f64 } else { 0.0 }
    );
    println!("RECV: DONE session={session_id}");
}
