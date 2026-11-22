//! W1 Spike：采集 → 编码 端到端延迟实测（PRD §8）。
//!
//! 输出每阶段耗时，用来回答「40ms 预算够不够」。
use std::time::Instant;

use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, OpenH264Codec};

fn main() {
    let mut cap = match ScreenCapture::open_primary() {
        Ok(c) => c,
        Err(e) => {
            println!("NO_DISPLAY: {e}");
            return;
        }
    };
    let info = cap.info();
    println!("display: {}x{}", info.width, info.height);

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
            println!("CODEC_INIT_FAIL: {e}");
            return;
        }
    };

    let mut capture_ms = Vec::new();
    let mut encode_ms = Vec::new();
    let mut bytes = Vec::new();

    for i in 0..10 {
        let t0 = Instant::now();
        let frame = match cap.grab() {
            Ok(f) => f,
            Err(e) => {
                println!("CAPTURE_FAIL: {e}");
                return;
            }
        };
        capture_ms.push(t0.elapsed().as_secs_f64() * 1000.0);

        // 尺寸对齐到偶数（H.264 要求）
        let frame = if frame.width != cfg.width || frame.height != cfg.height {
            println!(
                "size mismatch {}x{} vs cfg {}x{}",
                frame.width, frame.height, cfg.width, cfg.height
            );
            return;
        } else {
            frame
        };

        let t1 = Instant::now();
        match codec.encode_bgra(&frame.data, frame.ts_ms) {
            Ok(pkt) => {
                encode_ms.push(t1.elapsed().as_secs_f64() * 1000.0);
                bytes.push(pkt.len());
                if i == 0 || i == 9 {
                    println!(
                        "frame {i}: capture {:.1}ms encode {:.1}ms {} bytes",
                        capture_ms[i],
                        encode_ms[i],
                        pkt.len()
                    );
                }
            }
            Err(e) => {
                println!("ENCODE_FAIL: {e}");
                return;
            }
        }
    }

    let avg = |v: &Vec<f64>| v.iter().sum::<f64>() / v.len() as f64;
    let max = |v: &Vec<f64>| v.iter().cloned().fold(0.0, f64::max);
    println!("---");
    println!(
        "capture avg {:.2}ms max {:.2}ms",
        avg(&capture_ms),
        max(&capture_ms)
    );
    println!(
        "encode  avg {:.2}ms max {:.2}ms",
        avg(&encode_ms),
        max(&encode_ms)
    );
    println!(
        "pipeline avg {:.2}ms (capture+encode)",
        avg(&capture_ms) + avg(&encode_ms)
    );
    println!(
        "bitrate  avg {:.0} kbps",
        bytes.iter().sum::<usize>() as f64 * 8.0 / 1000.0 / bytes.len() as f64 * 60.0
    );
}
