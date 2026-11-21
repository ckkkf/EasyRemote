//! 端到端：真实 Go 中继 + 两个 Rust 客户端。
//!
//! 被控端：采集 → 编码 → 中继 → 主控端 → 解码。
//! 这是 PRD §7.2.3 数据面的最小闭环验证。

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, H264Decoder, OpenH264Codec};
use er_session::relay_client::RelayClient;

const SECRET: &[u8] = b"dev-secret-change-me";

fn main() {
    let relay: SocketAddr = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "127.0.0.1:19101".to_string())
        .parse()
        .expect("relay addr");

    let sid = "ses-e2e-1";
    let host = RelayClient::new(relay, sid, "devA").expect("host client");
    let mut ctrl = RelayClient::new(relay, sid, "devB").expect("ctrl client");

    host.register_with_secret(SECRET, "devB", 600).expect("host register");
    ctrl.register_with_secret(SECRET, "devA", 600).expect("ctrl register");
    std::thread::sleep(Duration::from_millis(400));

    // 采集 + 编码
    let mut cap = match ScreenCapture::open_primary() {
        Ok(c) => c,
        Err(e) => {
            println!("NO_DISPLAY: {e}");
            return;
        }
    };
    let info = cap.info();
    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    let mut codec = match OpenH264Codec::new(cfg) {
        Ok(c) => c,
        Err(e) => {
            println!("CODEC_FAIL: {e}");
            return;
        }
    };
    let frame = match cap.grab() {
        Ok(f) => f,
        Err(e) => {
            println!("CAPTURE_FAIL: {e}");
            return;
        }
    };
    let encoded = match codec.encode_bgra(&frame.data, frame.ts_ms) {
        Ok(d) => d,
        Err(e) => {
            println!("ENCODE_FAIL: {e}");
            return;
        }
    };
    println!("source {}x{}, encoded {} bytes", frame.width, frame.height, encoded.len());

    // 发送（自动分片）
    let mut host = host;
    let frags = host.send(&encoded).expect("send");
    println!("sent in {frags} fragment(s)");

    // 接收 + 解码
    let msg = match ctrl.recv_timeout(Duration::from_secs(3)) {
        Ok(Some(m)) => m,
        Ok(None) => {
            println!("FAIL: no data received through relay");
            std::process::exit(1);
        }
        Err(e) => {
            println!("RECV_FAIL: {e}");
            std::process::exit(1);
        }
    };
    println!("received {} bytes via relay", msg.len());
    if msg != encoded {
        println!("FAIL: payload mismatch after relay");
        std::process::exit(1);
    }

    let mut dec = match H264Decoder::new() {
        Ok(d) => d,
        Err(e) => {
            println!("DECODER_FAIL: {e}");
            return;
        }
    };
    match dec.decode(&msg) {
        Ok(Some((w, h, rgb))) => {
            println!("decoded {w}x{h}, {} rgb bytes", rgb.len());
            if w != frame.width as usize || h != frame.height as usize {
                println!("FAIL: dimension mismatch ({w}x{h} vs {}x{})", frame.width, frame.height);
                std::process::exit(1);
            }
            if !rgb.iter().any(|b| *b != 0) {
                println!("FAIL: decoded pixels are all zero");
                std::process::exit(1);
            }
            println!("RELAY E2E PASSED");
        }
        Ok(None) => {
            println!("FAIL: decoder returned nothing");
            std::process::exit(1);
        }
        Err(e) => {
            println!("DECODE_FAIL: {e}");
            std::process::exit(1);
        }
    }
}
