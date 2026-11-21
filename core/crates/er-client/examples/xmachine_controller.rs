//! 跨机验证 · 主控端（Mac）。
//!
//! 全链路：真实 gateway → 真实 rendezvous → 真实 relay → 采集/合成帧 → 编码 → 加密 → 中继；
//! 反向：收被控端（Windows）真实屏幕帧 → 解码；再发输入事件给被控端注入。
//!
//! 顺序（上一轮在这里踩坑）：**先发输入、再收帧**。
//! 被控端发完帧就进 `recv_input`；如果主控还堵在 `recv_frames` 里等帧，
//! 被控端就会空等（静止桌面只出 1 帧的场景下必现）。
//!
//! 用法：
//!   xmachine_controller <gateway> <relay_udp> <phone> <sms> <hello_file> <out_frames> <in_frames> [move_x move_y]

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_client::agent::{encode_video_msg, ControllerAgent, MSG_VIDEO};
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

/// 合成渐变帧 → H.264 → 加密 → 中继（主控屏录未授权时的真实链路替代）。
fn send_synthetic(relay: &mut RelayClient, crypto: &mut CryptoSession, count: usize) -> Vec<usize> {
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

fn main() {
    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700005202");
    let sms = arg(4, "123456");
    let hello_file = arg(5, "win_hello.txt");
    let out_frames: usize = arg(6, "5").parse().expect("out_frames");
    let in_frames: usize = arg(7, "5").parse().expect("in_frames");
    let move_x: i32 = arg(8, "400").parse().expect("move_x");
    let move_y: i32 = arg(9, "300").parse().expect("move_y");

    // 设备码/验证码由被控端写出（真实产品里是显示在被控端屏幕上）
    let deadline = std::time::Instant::now() + Duration::from_secs(180);
    let hello = loop {
        if let Ok(s) = std::fs::read_to_string(&hello_file) {
            if s.contains("captcha=") {
                break s;
            }
        }
        if std::time::Instant::now() > deadline {
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
    println!("MAC: gateway={base} relay={relay_addr} device_code={code} captcha={captcha}");

    let ctrl = ControllerAgent::start(&base, &phone, &sms, relay_addr).expect("ctrl start");
    let session_id = ctrl
        .request_session(&code, &captcha)
        .expect("request session");
    println!("MAC: session={session_id}");
    // 等被控端 consent 落地（真实产品里用户点「接受」也要时间），否则取票据会被拒
    std::thread::sleep(Duration::from_millis(1500));

    let (mut relay, mut crypto, keys, msg) = ctrl.begin(&session_id).expect("ctrl begin");
    println!("MAC: relay_registered local={}", relay.local_addr());
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay, &msg).expect("announce");
    ctrl.finish(&mut relay, &mut crypto, &keys, &session_id)
        .expect("handshake");
    println!("MAC: handshake_verified (ed25519 + x25519)");

    // ① 发给被控端视频帧
    match ScreenCapture::open_primary() {
        Ok(mut cap) => {
            let info = cap.info();
            println!("MAC: capture_ok {}x{}", info.width, info.height);
            let sizes = ctrl
                .send_frames(&mut relay, &mut crypto, out_frames)
                .expect("send frames");
            println!(
                "MAC: frames_sent={} bytes_total={}",
                sizes.len(),
                sizes.iter().sum::<usize>()
            );
        }
        Err(e) => {
            println!("MAC: capture_unavailable ({e}); fallback=synthetic_h264");
            let sizes = send_synthetic(&mut relay, &mut crypto, out_frames);
            println!(
                "MAC: frames_sent={} bytes_total={}",
                sizes.len(),
                sizes.iter().sum::<usize>()
            );
        }
    }

    // ② 立刻发输入：被控端收完帧就进 recv_input，不能让它在那边空等
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
    let n = ctrl
        .send_input(&mut relay, &mut crypto, &events)
        .expect("send input");
    println!("MAC: input_events_sent={n} target=({move_x},{move_y}) sent_before_recv=true");

    // ③ 收被控端（Windows）采集编码的真实屏幕帧并解码
    let got = ctrl
        .recv_frames(&mut relay, &mut crypto, in_frames)
        .expect("recv frames");
    println!(
        "MAC: frames_from_target={} resolution={}x{} first_h264_bytes={}",
        got.len(),
        got[0].width,
        got[0].height,
        got[0].h264_bytes
    );
    println!("MAC: MSG_VIDEO={MSG_VIDEO} (帧头 seq/ts/dim 已随帧传输)");
    println!("MAC: DONE session={session_id}");
}
