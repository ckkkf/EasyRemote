//! 弱网矩阵 · 主控侧发送器（Mac）。
//!
//! 在真实 gateway+relay 上：握手 → PING/PONG 估时钟偏移 → 连续发 N 帧真实 H.264
//! （合成图像，编码器走 er_codec::create_encoder）→ 发 DONE。
//!
//! 每帧报文：[1][idx u32][ts u64][offset i64][h264...]
//!   ts     = 发送端 epoch ms
//!   offset = 接收端时钟 - 发送端时钟（PING/PONG 估出来，只用来算单向时延）
//!
//! 用法：
//!   weaknet_send <gateway> <relay_udp> <phone> <sms> <hello_file> <frames> <pace_ms> <label>

use std::net::SocketAddr;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use er_client::agent::ControllerAgent;
use er_codec::{create_encoder, EncodeConfig, Encoder};

const MSG_VIDEO: u8 = 1;
const MSG_PING: u8 = 0x10;
const MSG_PONG: u8 = 0x11;
const MSG_DONE: u8 = 0x12;

fn arg(i: usize, default: &str) -> String {
    std::env::args()
        .nth(i)
        .unwrap_or_else(|| default.to_string())
}
fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

fn main() {
    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700006202");
    let sms = arg(4, "123456");
    let hello_file = arg(5, "win_hello.txt");
    let frames: u32 = arg(6, "300").parse().expect("frames");
    let pace_ms: u64 = arg(7, "16").parse().expect("pace_ms");
    let label = arg(8, "run");
    let w: u32 = arg(9, "320").parse().expect("width");
    let h: u32 = arg(10, "240").parse().expect("height");

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
        std::thread::sleep(Duration::from_millis(200));
    };
    let field = |k: &str| -> String {
        for line in hello.lines() {
            if let Some(rest) = line.strip_prefix(k) {
                return rest.trim().to_string();
            }
        }
        panic!("missing {k}");
    };
    let code = field("device_code=");
    let captcha = field("captcha=");
    println!("SEND[{label}]: device_code={code} captcha={captcha}");

    let ctrl = ControllerAgent::start(&base, &phone, &sms, relay_addr).expect("ctrl start");
    let session_id = ctrl
        .request_session(&code, &captcha)
        .expect("request session");
    println!("SEND[{label}]: session={session_id}");
    std::thread::sleep(Duration::from_millis(1500));
    // 被控端 consent 落地有延迟；取票据被拒时重试（不改变任何断言，只是等待）
    let (mut relay, mut crypto, keys, msg) = {
        let mut attempt = 0;
        loop {
            match ctrl.begin(&session_id) {
                Ok(v) => break v,
                Err(e) => {
                    attempt += 1;
                    if attempt >= 12 {
                        panic!("ctrl begin failed after {attempt} attempts: {e}");
                    }
                    std::thread::sleep(Duration::from_millis(500));
                }
            }
        }
    };
    std::thread::sleep(Duration::from_millis(2500));
    let mut hs_attempt = 0;
    loop {
        ctrl.announce(&mut relay, &msg).expect("announce");
        match ctrl.finish(&mut relay, &mut crypto, &keys, &session_id) {
            Ok(()) => break,
            Err(e) => {
                hs_attempt += 1;
                if hs_attempt >= 3 {
                    panic!("handshake failed after {hs_attempt} attempts: {e}");
                }
                println!("SEND[{label}]: handshake_retry={hs_attempt} ({e})");
            }
        }
    }
    println!("SEND[{label}]: handshake_verified");

    // 时钟偏移（PING/PONG）：只用它把单向时延算对，不做别的用途
    let mut offset: i64 = 0;
    let mut rtt_ms: u64 = 0;
    let mut ping_ok = false;
    for _ in 0..5 {
        let t0 = now_ms();
        let mut p = vec![MSG_PING];
        p.extend_from_slice(&t0.to_le_bytes());
        let sealed = crypto.seal(&p);
        if relay.send(&sealed).is_err() {
            continue;
        }
        let wait_until = Instant::now() + Duration::from_secs(2);
        while Instant::now() < wait_until {
            match relay.recv_timeout(Duration::from_millis(300)) {
                Ok(Some(s)) => {
                    let Ok(plain) = crypto.open(&s) else { continue };
                    if plain.first() != Some(&MSG_PONG) || plain.len() < 17 {
                        continue;
                    }
                    let echo = u64::from_le_bytes(plain[1..9].try_into().unwrap());
                    if echo != t0 {
                        continue;
                    }
                    let t1 = u64::from_le_bytes(plain[9..17].try_into().unwrap());
                    let t2 = now_ms();
                    rtt_ms = t2.saturating_sub(t0);
                    offset = t1 as i64 - (t0 as i64 + (rtt_ms as i64) / 2);
                    ping_ok = true;
                    break;
                }
                _ => {}
            }
        }
        if ping_ok {
            break;
        }
    }
    println!("SEND[{label}]: clock rtt_ms={rtt_ms} offset_ms={offset} ok={ping_ok}");

    let cfg = EncodeConfig {
        width: w,
        height: h,
        fps: 60,
        bitrate_kbps: 1500,
        gop: 60,
        bframes: 0,
    };
    let mut codec = create_encoder(cfg).expect("encoder");
    println!("SEND[{label}]: encoder_kind={:?} {w}x{h}", codec.kind());

    let mut frame = vec![0u8; (w * h * 4) as usize];
    let mut enc_ms_total = 0f64;
    let mut bytes_total = 0usize;
    let mut actually_sent = 0u32;
    let t_start = Instant::now();
    for i in 0..frames {
        for y in 0..h {
            for x in 0..w {
                let idx = ((y * w + x) * 4) as usize;
                frame[idx] = ((x + i * 3) % 256) as u8;
                frame[idx + 1] = ((y + i * 5) % 256) as u8;
                frame[idx + 2] = ((x ^ y ^ (i * 7)) % 256) as u8;
                frame[idx + 3] = 255;
            }
        }
        let t_enc = Instant::now();
        let pkt = codec.encode(i as u64 * pace_ms, &frame).expect("encode");
        enc_ms_total += t_enc.elapsed().as_secs_f64() * 1000.0;
        if pkt.data.is_empty() {
            continue;
        }
        let mut m = Vec::with_capacity(pkt.data.len() + 21);
        m.push(MSG_VIDEO);
        m.extend_from_slice(&i.to_le_bytes());
        m.extend_from_slice(&now_ms().to_le_bytes());
        m.extend_from_slice(&offset.to_le_bytes());
        m.extend_from_slice(&pkt.data);
        bytes_total += pkt.data.len();
        let sealed = crypto.seal(&m);
        relay.send(&sealed).expect("relay send");
        actually_sent += 1;
        std::thread::sleep(Duration::from_millis(pace_ms));
    }
    let elapsed = t_start.elapsed().as_millis();

    let mut d = vec![MSG_DONE];
    d.extend_from_slice(&frames.to_le_bytes());
    let sealed = crypto.seal(&d);
    let _ = relay.send(&sealed);

    println!(
        "SEND[{label}]: frames_sent={actually_sent} frames_target={frames} bytes_total={bytes_total} encode_ms_avg={:.3} elapsed_ms={elapsed}",
        enc_ms_total / frames.max(1) as f64
    );
    println!("SEND[{label}]: DONE session={session_id}");
    std::thread::sleep(Duration::from_millis(500));
}
