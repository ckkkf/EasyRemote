//! NVENC vs OpenH264 同帧对比（Windows / NVIDIA 真机验证）。
//!
//! 做三件事：
//!   1. 运行时探测：显卡名（NVML）、驱动版本、NVENC API 版本、H.264 会话能否开出；
//!   2. 用**同一帧**分别喂硬件编码器与软件编码器，各跑 N 次，打印单帧耗时；
//!   3. 把两路输出都用同一个 H.264 解码器解回来，校验尺寸与原帧一致。
//!
//! 用法：nvenc_bench.exe [width] [height] [iters]

use std::time::Instant;

use er_capture::{Capture, ScreenCapture};
use er_codec::{create_encoder, EncodeConfig, Encoder, EncoderKind, H264Decoder};

fn main() {
    let w: u32 = std::env::args().nth(1).and_then(|s| s.parse().ok()).unwrap_or(1280);
    let h: u32 = std::env::args().nth(2).and_then(|s| s.parse().ok()).unwrap_or(720);
    let iters: usize = std::env::args().nth(3).and_then(|s| s.parse().ok()).unwrap_or(20);

    println!("=== 环境 ===");
    #[cfg(target_os = "windows")]
    {
        let p = er_codec::nvenc::probe();
        println!("gpu             = {:?}", p.gpu_name);
        println!("driver          = {:?}", p.driver_version);
        println!("nvenc_api_max   = 0x{:08x}", p.max_api_version);
        println!("nvenc_available = {}", p.available);
        println!("probe_detail    = {}", p.detail);
    }
    #[cfg(not(target_os = "windows"))]
    {
        println!("platform        = non-windows (NVENC backend not compiled)");
    }
    println!("hardware_available = {}", er_codec::hardware_encoder_available());

    // 取一帧真实屏幕；拿不到（Session 0 / 无显示）就用合成帧，保证可跑
    let (frame, w, h, src) = match ScreenCapture::open_primary() {
        Ok(mut cap) => {
            match cap.grab() {
                Ok(f) => {
                    let (w, h) = (f.width & !1, f.height & !1);
                    (crop(&f.data, f.width, f.height, w, h), w, h, "screen")
                }
                Err(_) => (synthetic(w, h), w, h, "synthetic"),
            }
        }
        Err(_) => (synthetic(w, h), w, h, "synthetic"),
    };
    println!("frame_source={src} size={w}x{h} bytes={}", frame.len());

    let cfg = EncodeConfig { width: w, height: h, fps: 60, bitrate_kbps: 4000, gop: 60, bframes: 0 };

    // ---- 硬件 ----
    let mut hw = create_encoder(cfg).expect("create hardware encoder");
    let hw_kind = hw.kind();
    println!("\n--- 硬件路径: {:?} ---", hw_kind);
    if hw_kind != EncoderKind::Nvenc {
        println!("NVENC 不可用，create_encoder 回退到 {:?}", hw_kind);
    }
    let (hw_ms, hw_idr_bytes, hw_last_bytes, hw_pkt) = time_encoder(hw.as_mut(), &frame, w, h, iters);
    println!(
        "hw encode: {:.3} ms/frame, {:.1} fps, idr={} bytes last={} bytes",
        hw_ms,
        1000.0 / hw_ms.max(0.001),
        hw_idr_bytes,
        hw_last_bytes
    );

    // ---- 软件 ----
    let mut sw = Box::new(er_codec::OpenH264Codec::new(cfg).expect("software encoder")) as Box<dyn Encoder>;
    println!("\n--- 软件路径: {:?} ---", sw.kind());
    let (sw_ms, sw_idr_bytes, sw_last_bytes, sw_pkt) = time_encoder(sw.as_mut(), &frame, w, h, iters);
    println!(
        "sw encode: {:.3} ms/frame, {:.1} fps, idr={} bytes last={} bytes",
        sw_ms,
        1000.0 / sw_ms.max(0.001),
        sw_idr_bytes,
        sw_last_bytes
    );

    // ---- 解码校验 ----
    println!("\n--- 解码校验 ---");
    let mut d1 = H264Decoder::new().expect("decoder");
    let hw_dec = d1.decode(&hw_pkt).expect("hw decode");
    let hw_dims = match &hw_dec {
        Some((dw, dh, _)) => {
            println!("hw decoded: {dw}x{dh} (rgb bytes {})", dw * dh * 3);
            Some((*dw, *dh))
        }
        None => {
            println!("hw decoded: none");
            None
        }
    };
    let mut d2 = H264Decoder::new().expect("decoder");
    let sw_dec = d2.decode(&sw_pkt).expect("sw decode");
    let sw_dims = match &sw_dec {
        Some((dw, dh, _)) => {
            println!("sw decoded: {dw}x{dh} (rgb bytes {})", dw * dh * 3);
            Some((*dw, *dh))
        }
        None => {
            println!("sw decoded: none");
            None
        }
    };

    println!("\n=== 对比 ===");
    println!("{:<12} {:>12} {:>12}", "编码器", "ms/frame", "fps");
    println!("{:<12} {:>12.3} {:>12.1}", "Nvenc/SW", hw_ms, 1000.0 / hw_ms.max(0.001));
    println!("{:<12} {:>12.3} {:>12.1}", "OpenH264", sw_ms, 1000.0 / sw_ms.max(0.001));
    println!("speedup = {:.2}x", sw_ms / hw_ms.max(0.001));

    let dims_ok = hw_dims == Some((w as usize, h as usize)) && sw_dims == Some((w as usize, h as usize));
    let real_hw = hw_kind == EncoderKind::Nvenc;
    println!("\nRESULT: hw_kind={:?} hw_dims_ok={} sw_dims_ok={} dims_match={} ",
        hw_kind, hw_dims == Some((w as usize, h as usize)), sw_dims == Some((w as usize, h as usize)), dims_ok);
    if !dims_ok || !real_hw {
        println!("VERDICT: FAIL (hardware={} dims_ok={})", real_hw, dims_ok);
        std::process::exit(1);
    }
    println!("VERDICT: PASS (NVENC 真实启用，同帧硬件编码耗时 {:.2}ms vs 软件 {:.2}ms，解码尺寸 {w}x{h} 一致)", hw_ms, sw_ms);
}

fn crop(src: &[u8], sw: u32, _sh: u32, w: u32, h: u32) -> Vec<u8> {
    let mut out = vec![0u8; (w * h * 4) as usize];
    for y in 0..h as usize {
        let src_off = (y * sw as usize) * 4;
        let dst_off = (y * w as usize) * 4;
        let n = (w as usize) * 4;
        out[dst_off..dst_off + n].copy_from_slice(&src[src_off..src_off + n]);
    }
    out
}

fn synthetic(w: u32, h: u32) -> Vec<u8> {
    let mut f = vec![0u8; (w * h * 4) as usize];
    for y in 0..h {
        for x in 0..w {
            let i = ((y * w + x) * 4) as usize;
            f[i] = (x % 256) as u8;
            f[i + 1] = (y % 256) as u8;
            f[i + 2] = ((x ^ y) % 256) as u8;
            f[i + 3] = 255;
        }
    }
    f
}

fn time_encoder(enc: &mut dyn Encoder, frame: &[u8], _w: u32, _h: u32, iters: usize) -> (f64, usize, usize, Vec<u8>) {
    // 预热一帧（首帧要出 SPS/PPS + 建立参考帧）
    let first = enc.encode(0, frame).expect("warmup encode");
    let mut total = 0f64;
    let mut n = 0usize;
    let mut last_bytes = first.data.len();
    for i in 0..iters {
        let t = Instant::now();
        let p = enc.encode((i as u64 + 1) * 16, frame).expect("encode");
        let dt = t.elapsed().as_secs_f64() * 1000.0;
        total += dt;
        n += 1;
        last_bytes = p.data.len();
    }
    // 解码校验用第一个 IDR 包
    (total / n as f64, first.data.len(), last_bytes, first.data)
}
