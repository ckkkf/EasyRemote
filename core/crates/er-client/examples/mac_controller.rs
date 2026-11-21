//! macOS 主控端实跑：用设备码 + 验证码发起 → 握手 → 发帧 → 接收被控端真实帧并解码。
//!
//! 用法：
//!   mac_controller <gateway> <relay_addr> <phone> <sms> <device_code> <captcha> <out_frames> <in_frames> <wait_hello>
//!
//! 主屏采集在 macOS 需要「屏幕录制」授权；未授权时退化为合成帧（仍然是真实
//! 编码 → 加密 → 中继 → 解密 → 解码链路），并明确打印走了哪条路。

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_client::agent::ControllerAgent;
use er_codec::{EncodeConfig, OpenH264Codec};
use er_session::crypto::CryptoSession;
use er_session::relay_client::RelayClient;

use er_client::agent::{encode_video_msg, MSG_VIDEO};

fn arg(i: usize, default: &str) -> String {
    std::env::args().nth(i).unwrap_or_else(|| default.to_string())
}

/// 合成渐变帧 → H.264 → 加密 → 中继。用于主屏授权不可用的机器。
fn send_synthetic(relay: &mut RelayClient, crypto: &mut CryptoSession, count: usize) -> Vec<usize> {
    let (w, h) = (320u32, 240u32);
    let cfg = EncodeConfig { width: w, height: h, fps: 60, bitrate_kbps: 1000, gop: 60, bframes: 0 };
    let mut codec = OpenH264Codec::new(cfg).expect("codec");
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
        let encoded = codec.encode_bgra(&frame, (i as u64) * 16).expect("encode");
        if encoded.is_empty() {
            continue;
        }
        let hdr = er_client::link::VideoHeader {
            seq: i as u32,
            ts_ms: (i as u64 * 16) as u32,
            width: w as u16,
            height: h as u16,
        };
        let msg = encode_video_msg(&hdr, &encoded);
        let sealed = crypto.seal(&msg);
        relay.send(&sealed).expect("relay send");
        sizes.push(encoded.len());
        std::thread::sleep(Duration::from_millis(16));
    }
    sizes
}

fn main() {
    let base = arg(1, "http://127.0.0.1:8090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:9000").parse().expect("relay addr");
    let phone = arg(3, "13700004201");
    let sms = arg(4, "123456");
    let wait_hello = arg(5, "");
    let out_frames: usize = arg(7, "5").parse().expect("out_frames");
    let in_frames: usize = arg(8, "5").parse().expect("in_frames");

    // 设备码/验证码来自被控端写出的 hello 文件（真实产品里是屏幕上的数字）
    let hello = if wait_hello.is_empty() {
        String::new()
    } else {
        let deadline = std::time::Instant::now() + Duration::from_secs(120);
        loop {
            if let Ok(s) = std::fs::read_to_string(&wait_hello) {
                if s.contains("captcha=") {
                    break s;
                }
            }
            if std::time::Instant::now() > deadline {
                panic!("hello file never appeared: {wait_hello}");
            }
            std::thread::sleep(Duration::from_millis(300));
        }
    };
    let field = |k: &str| -> String {
        if let Some(v) = std::env::args().nth(if k == "code" { 5 } else { 6 }) {
            if !v.is_empty() && v != "-" {
                return v;
            }
        }
        for line in hello.lines() {
            let key = match k {
                "code" => "device_code=",
                _ => "captcha=",
            };
            if let Some(rest) = line.strip_prefix(key) {
                return rest.trim().to_string();
            }
        }
        panic!("missing {k}");
    };
    let code = field("code");
    let captcha = field("captcha");
    println!("MAC: gateway={base} relay={relay_addr} device_code={code} captcha={captcha}");

    let ctrl = ControllerAgent::start(&base, &phone, &sms, relay_addr).expect("ctrl start");
    let session_id = ctrl.request_session(&code, &captcha).expect("request session");
    println!("MAC: session={session_id}");

    let (mut relay, mut crypto, keys, msg) = ctrl.begin(&session_id).expect("ctrl begin");
    println!("MAC: relay registered");
    // 等被控端注册中继（否则先发的握手会被中继丢弃）
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay, &msg).expect("announce controller handshake");
    ctrl.finish(&mut relay, &mut crypto, &keys, &session_id).expect("handshake verify");
    println!("MAC: handshake verified (ed25519 + x25519)");

    // ① 发给被控端真实视频帧：优先真实采集，屏录未授权时退化为合成帧
    match ScreenCapture::open_primary() {
        Ok(mut cap) => {
            let info = cap.info();
            println!("MAC: capture_ok {}x{}", info.width, info.height);
            let _ = (cap.grab().is_ok(),);
            let sizes = ctrl.send_frames(&mut relay, &mut crypto, out_frames).expect("send frames");
            println!("MAC: frames_sent={} bytes_total={}", sizes.len(), sizes.iter().sum::<usize>());
        }
        Err(e) => {
            println!("MAC: capture_unavailable ({e}); fallback=synthetic_h264");
            let sizes = send_synthetic(&mut relay, &mut crypto, out_frames);
            println!("MAC: frames_sent={} bytes_total={}", sizes.len(), sizes.iter().sum::<usize>());
        }
    }

    // ② 接收被控端（Windows）采集编码后的真实帧并解码
    let got = ctrl.recv_frames(&mut relay, &mut crypto, in_frames).expect("recv frames");
    println!(
        "MAC: frames_from_target={} resolution={}x{} first_h264_bytes={}",
        got.len(),
        got[0].width,
        got[0].height,
        got[0].h264_bytes
    );
    println!("MAC: DONE session={session_id}");
}
