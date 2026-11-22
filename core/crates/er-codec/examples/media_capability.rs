//! ER-600 media capability baseline: capture + encode on the current host.
//! Run: cargo run --release -p er-codec --example media_capability
use std::time::Instant;

use er_capture::{Capture, ScreenCapture};
use er_codec::{create_encoder, EncodeConfig};

fn main() {
    let mut cap = ScreenCapture::open_primary().expect("capture");
    let info = cap.info();
    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    let mut enc = create_encoder(cfg).expect("encoder");
    let kind = enc.kind();
    let frames = 120u32;
    let start = Instant::now();
    let mut bytes = 0usize;
    let mut encoded = 0u32;
    for i in 0..frames {
        let f = cap.grab().expect("grab");
        if let Ok(p) = enc.encode(f.ts_ms + i as u64, &f.data) {
            bytes += p.data.len();
            encoded += 1;
        }
    }
    let elapsed = start.elapsed();
    let fps = encoded as f64 / elapsed.as_secs_f64();
    println!(
        "{{\"backend\":\"{:?}\",\"width\":{},\"height\":{},\"requested_fps\":{},\"encoded\":{},\"elapsed_ms\":{:.1},\"fps\":{:.1},\"mbps\":{:.1}}}",
        kind,
        cfg.width,
        cfg.height,
        60,
        encoded,
        elapsed.as_secs_f64() * 1000.0,
        fps,
        bytes as f64 * 8.0 / elapsed.as_secs_f64() / 1_000_000.0
    );
}
