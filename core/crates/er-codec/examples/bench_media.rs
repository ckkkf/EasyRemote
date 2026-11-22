//! ER-901 媒体基准：合成 1080p BGRA，循环编码+解码，输出 P50/P95 与 FPS。
//!
//! 时长由 ER_BENCH_SECONDS 控制（默认 10 秒；30min soak 用 1800）。

use std::time::{Duration, Instant};

use er_codec::{EncodeConfig, H264Decoder, OpenH264Codec};

fn percentile(sorted: &[f64], p: f64) -> f64 {
    if sorted.is_empty() {
        return 0.0;
    }
    let idx = ((sorted.len() as f64 - 1.0) * p).round() as usize;
    sorted[idx.min(sorted.len() - 1)]
}

fn main() {
    let secs: u64 = std::env::var("ER_BENCH_SECONDS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(10);
    let (w, h) = (1920usize, 1080usize);
    let cfg = EncodeConfig {
        width: w as u32,
        height: h as u32,
        fps: 30,
        bitrate_kbps: 4000,
        gop: 30,
        bframes: 0,
    };
    let mut enc = OpenH264Codec::new(cfg).expect("encoder");
    let mut dec = H264Decoder::new().expect("decoder");
    let mut frame = vec![0u8; w * h * 4];
    for y in 0..h {
        for x in 0..w {
            let i = (y * w + x) * 4;
            frame[i] = (x % 256) as u8;
            frame[i + 1] = (y % 256) as u8;
            frame[i + 2] = ((x + y) % 256) as u8;
            frame[i + 3] = 255;
        }
    }

    let start = Instant::now();
    let mut encode_ms = Vec::new();
    let mut decode_ms = Vec::new();
    let mut encoded_bytes = 0u64;
    let mut frames = 0u64;
    let mut ts = 0u64;
    while start.elapsed() < Duration::from_secs(secs) {
        let t0 = Instant::now();
        let encoded = match enc.encode_bgra(&frame, ts) {
            Ok(v) => v,
            Err(e) => {
                eprintln!("encode error: {e}");
                std::process::exit(1);
            }
        };
        encode_ms.push(t0.elapsed().as_secs_f64() * 1000.0);
        encoded_bytes += encoded.len() as u64;
        let t1 = Instant::now();
        let _ = dec.decode(&encoded);
        decode_ms.push(t1.elapsed().as_secs_f64() * 1000.0);
        frames += 1;
        ts = ts.wrapping_add(33);
    }
    encode_ms.sort_by(|a, b| a.partial_cmp(b).unwrap());
    decode_ms.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let elapsed = start.elapsed().as_secs_f64();
    let fps = frames as f64 / elapsed;
    let kbps = encoded_bytes as f64 * 8.0 / elapsed / 1000.0;
    println!(
        "{{\"frames\":{},\"seconds\":{:.2},\"fps\":{:.1},\"bitrate_kbps\":{:.0},\"encode_p50_ms\":{:.2},\"encode_p95_ms\":{:.2},\"decode_p50_ms\":{:.2},\"decode_p95_ms\":{:.2}}}",
        frames,
        elapsed,
        fps,
        kbps,
        percentile(&encode_ms, 0.50),
        percentile(&encode_ms, 0.95),
        percentile(&decode_ms, 0.50),
        percentile(&decode_ms, 0.95),
    );
}
