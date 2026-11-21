//! DXGI / NVENC 多路径对比（Windows + NVIDIA 真机基准）。
//!
//! 目标：回答「采集是不是端到端主瓶颈，换成 DXGI + 零拷贝后到底降到多少」。
//!
//! 路径：
//!   A xcap + NVENC(copy)      : xcap 全帧 CPU 拷贝 -> cuMemcpy2D 上传 -> NVENC
//!   B dxgi + NVENC(copy)      : DXGI staging 读回 -> cuMemcpy2D 上传 -> NVENC
//!   C dxgi + NVENC(zero-copy) : DXGI 纹理 -> GPU CopyResource -> 常驻注册 -> NVENC
//!   D dxgi + NVENC(direct)    : DXGI 桌面纹理直接注册给 NVENC（连 GPU 拷贝都不做）
//!   E NVENC(directx) 纹理直编 : host 帧上传成 D3D11 纹理 -> 常驻注册 -> NVENC
//!                               （Desktop Duplication 不可用时用于单独验证 DIRECTX 输入路径）
//!
//! 每条路径都给 capture ms/frame、encode ms/frame、端到端 fps，并把首帧 IDR
//! 解回来校验尺寸。
//!
//! 用法：dxgi_nvenc_bench.exe [iters]

fn main() {
    #[cfg(target_os = "windows")]
    {
        if let Err(e) = windows_main() {
            println!("FAIL: {e}");
            std::process::exit(1);
        }
    }
    #[cfg(not(target_os = "windows"))]
    {
        println!("SKIP: DXGI/NVENC 基准只能在 Windows + NVIDIA 上跑");
    }
}

#[cfg(target_os = "windows")]
use std::time::Instant;

#[cfg(target_os = "windows")]
use er_capture::dxgi::DxgiCapture;
#[cfg(target_os = "windows")]
use er_capture::{Capture, RawFrame, ScreenCapture};
#[cfg(target_os = "windows")]
use er_codec::nvenc::{self, NvencEncoder};
#[cfg(target_os = "windows")]
use er_codec::{EncodeConfig, H264Decoder};

#[cfg(target_os = "windows")]
#[derive(Default)]
struct Report {
    frames: usize,
    cap_ms: f64,
    enc_ms: f64,
    aux_ms: f64,
    wall_ms: f64,
    max_cap: f64,
    max_enc: f64,
    idr: Vec<u8>,
    idr_bytes: usize,
    last_bytes: usize,
}

#[cfg(target_os = "windows")]
impl Report {
    fn new() -> Self {
        Self::default()
    }
    fn avg_cap(&self) -> f64 {
        self.cap_ms / self.frames.max(1) as f64
    }
    fn avg_enc(&self) -> f64 {
        self.enc_ms / self.frames.max(1) as f64
    }
    fn avg_aux(&self) -> f64 {
        self.aux_ms / self.frames.max(1) as f64
    }
    fn fps(&self) -> f64 {
        if self.wall_ms <= 0.0 {
            return 0.0;
        }
        self.frames as f64 * 1000.0 / self.wall_ms
    }
}

#[cfg(target_os = "windows")]
fn now_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

#[cfg(target_os = "windows")]
fn decode_dims(pkt: &[u8]) -> String {
    if pkt.is_empty() {
        return "empty".into();
    }
    let mut dec = match H264Decoder::new() {
        Ok(d) => d,
        Err(e) => return format!("decoder-init-error: {e}"),
    };
    match dec.decode(pkt) {
        Ok(Some((w, h, _))) => format!("{w}x{h}"),
        Ok(None) => "none".into(),
        Err(e) => format!("decode-error: {e}"),
    }
}

#[cfg(target_os = "windows")]
fn crop(src: &[u8], sw: u32, w: u32, h: u32) -> Vec<u8> {
    let mut out = vec![0u8; (w * h * 4) as usize];
    for y in 0..h as usize {
        let src_off = (y * sw as usize) * 4;
        let dst_off = (y * w as usize) * 4;
        let n = (w as usize) * 4;
        out[dst_off..dst_off + n].copy_from_slice(&src[src_off..src_off + n]);
    }
    out
}

#[cfg(target_os = "windows")]
fn wait_gpu(cap: &mut DxgiCapture, attempts: usize) -> Result<er_capture::dxgi::GpuFrame, String> {
    // 静态桌面下 DXGI 按设计不出帧（没有内容变化就没有 present）。
    // 基准要测的是「有变化时的采集延迟」，所以先制造一次指针事件再取帧，
    // 否则测到的是「等 DWM 的时间」而不是采集成本。
    for i in 0..attempts {
        cap.nudge_desktop();
        match cap.grab_gpu(60) {
            Ok(Some(f)) => return Ok(f),
            Ok(None) => {
                if i + 1 == attempts {
                    return Err("no DXGI frame (desktop idle / locked)".into());
                }
            }
            Err(e) => return Err(format!("grab_gpu: {e}")),
        }
    }
    Err("no DXGI frame".into())
}

#[cfg(target_os = "windows")]
fn wait_cpu(cap: &mut DxgiCapture, attempts: usize) -> Result<RawFrame, String> {
    for i in 0..attempts {
        cap.nudge_desktop();
        match cap.grab_cpu(60) {
            Ok(Some(f)) => return Ok(f),
            Ok(None) => {
                if i + 1 == attempts {
                    return Err("no DXGI frame (desktop idle / locked)".into());
                }
            }
            Err(e) => return Err(format!("grab_cpu: {e}")),
        }
    }
    Err("no DXGI frame".into())
}

#[cfg(target_os = "windows")]
fn windows_main() -> Result<(), String> {
    let iters: usize = std::env::args()
        .nth(1)
        .and_then(|s| s.parse().ok())
        .unwrap_or(20);

    println!("=== 环境 ===");
    let p = nvenc::probe();
    println!("gpu             = {:?}", p.gpu_name);
    println!("driver          = {:?}", p.driver_version);
    println!("nvenc_api_max   = 0x{:08x}", p.max_api_version);
    println!("nvenc_available = {}", p.available);
    println!("probe_detail    = {}", p.detail);
    if !p.available {
        return Err("NVENC 不可用".into());
    }

    println!("\n=== DXGI 拓扑 ===");
    for line in er_capture::dxgi::topology() {
        println!("{line}");
    }
    println!("\n=== DXGI DuplicateOutput 探测 ===");
    for line in er_capture::dxgi::probe_report() {
        println!("{line}");
    }

    let mut cap = DxgiCapture::open_primary_detailed().map_err(|e| format!("DXGI open: {e}"))?;
    let info = cap.info();
    println!(
        "\ndxgi adapter = {:?} size = {}x{} format = {} (B8G8R8A8_UNORM={})",
        cap.adapter_name(),
        info.width,
        info.height,
        cap.format(),
        er_capture::dxgi::FORMAT_BGRA8_UNORM
    );

    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 8000,
        gop: 60,
        bframes: 0,
    };
    println!(
        "encode cfg      = {}x{} @{}fps gop={}",
        cfg.width, cfg.height, cfg.fps, cfg.gop
    );

    // 先探一次：桌面是否真的在出帧
    println!("\n=== Desktop Duplication 取帧探测 ===");
    println!("wake_display = {}", DxgiCapture::wake_display());
    for i in 0..4 {
        let line = cap.acquire_debug(1000);
        println!("acquire #{i}: {line}");
        cap.release();
        println!("nudge(sendinput) #{i}: ok={}", cap.nudge_desktop());
        std::thread::sleep(std::time::Duration::from_millis(150));
    }
    let live = match cap.grab_gpu(300) {
        Ok(Some(_)) => {
            cap.release();
            true
        }
        _ => false,
    };
    println!("dxgi_live = {live}");
    let mut dxgi_blocked: Option<String> = None;
    if !live {
        dxgi_blocked = Some(
            "AcquireNextFrame 恒返回 DXGI_ERROR_WAIT_TIMEOUT：桌面没有向该输出提交新帧".into(),
        );
        println!("DXGI_CAPTURE_BLOCKED: {}", dxgi_blocked.clone().unwrap());
        println!("（B/C/D 三条依赖 DXGI 采集的路径跳过；A/E 仍会给出可验证数据）");
    }

    let mut reports: Vec<(&str, Report)> = Vec::new();

    // ---------------- A: xcap + NVENC(copy) ----------------
    let mut xcap_frame_data: Option<Vec<u8>> = None;
    {
        let mut xcap = ScreenCapture::open_primary_xcap().map_err(|e| format!("xcap open: {e}"))?;
        let (fw, fh) = (xcap.info().width, xcap.info().height);
        println!("\n--- A: xcap + NVENC(copy)  xcap_size={fw}x{fh} ---");
        let mut enc = NvencEncoder::new(cfg).map_err(|e| format!("A nvenc: {e}"))?;
        println!("A backend = {}", enc.input_backend_name());
        let mut rep = Report::new();
        let t_all = Instant::now();
        for i in 0..iters {
            let t0 = Instant::now();
            let frame = xcap.grab().map_err(|e| format!("A grab: {e}"))?;
            let cap_ms = t0.elapsed().as_secs_f64() * 1000.0;
            let data = if frame.width != cfg.width || frame.height != cfg.height {
                crop(&frame.data, frame.width, cfg.width, cfg.height)
            } else {
                frame.data
            };
            if xcap_frame_data.is_none() {
                xcap_frame_data = Some(data.clone());
            }
            let t1 = Instant::now();
            let pkt = enc
                .encode_bgra(&data, now_ms())
                .map_err(|e| format!("A encode: {e}"))?;
            let enc_ms = t1.elapsed().as_secs_f64() * 1000.0;
            if rep.idr.is_empty() {
                rep.idr = pkt.clone();
                rep.idr_bytes = pkt.len();
            }
            rep.last_bytes = pkt.len();
            rep.frames += 1;
            rep.cap_ms += cap_ms;
            rep.enc_ms += enc_ms;
            rep.max_cap = rep.max_cap.max(cap_ms);
            rep.max_enc = rep.max_enc.max(enc_ms);
            if i == 0 || i + 1 == iters {
                println!(
                    "A frame {i}: capture {cap_ms:.2}ms encode {enc_ms:.2}ms {} bytes",
                    pkt.len()
                );
            }
        }
        rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
        reports.push(("xcap+NVENC(copy)", rep));
    }

    // ---------------- B/C/D: 依赖 DXGI 采集 ----------------
    if live {
        // B: DXGI staging 读回 + NVENC(copy)
        println!("\n--- B: DXGI + NVENC(copy) ---");
        let mut enc = NvencEncoder::new(cfg).map_err(|e| format!("B nvenc: {e}"))?;
        println!("B backend = {}", enc.input_backend_name());
        let mut rep = Report::new();
        let t_all = Instant::now();
        for i in 0..iters {
            let t0 = Instant::now();
            let frame = wait_cpu(&mut cap, 5)?;
            let cap_ms = t0.elapsed().as_secs_f64() * 1000.0;
            if !frame.is_well_formed() {
                return Err(format!(
                    "B frame malformed {}x{} {} bytes",
                    frame.width,
                    frame.height,
                    frame.data.len()
                ));
            }
            let t1 = Instant::now();
            let pkt = enc
                .encode_bgra(&frame.data, now_ms())
                .map_err(|e| format!("B encode: {e}"))?;
            let enc_ms = t1.elapsed().as_secs_f64() * 1000.0;
            if rep.idr.is_empty() {
                rep.idr = pkt.clone();
                rep.idr_bytes = pkt.len();
            }
            rep.last_bytes = pkt.len();
            rep.frames += 1;
            rep.cap_ms += cap_ms;
            rep.enc_ms += enc_ms;
            rep.max_cap = rep.max_cap.max(cap_ms);
            rep.max_enc = rep.max_enc.max(enc_ms);
            if i == 0 || i + 1 == iters {
                println!(
                    "B frame {i}: capture {cap_ms:.2}ms encode {enc_ms:.2}ms {} bytes",
                    pkt.len()
                );
            }
        }
        rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
        reports.push(("dxgi+NVENC(copy)", rep));

        // C: DXGI 纹理 -> GPU blit -> 常驻注册 -> NVENC
        println!("\n--- C: DXGI + NVENC(zero-copy, 常驻注册) ---");
        {
            let mut enc = NvencEncoder::new_d3d11(cfg, cap.device_raw())
                .map_err(|e| format!("C nvenc d3d11: {e}"))?;
            println!("C backend = {}", enc.input_backend_name());
            let tex = cap
                .ensure_encoder_input(cfg.width, cfg.height)
                .map_err(|e| format!("C ensure_encoder_input: {e}"))?;
            enc.register_d3d11_input(tex)
                .map_err(|e| format!("C register_d3d11_input: {e}"))?;
            let mut rep = Report::new();
            let t_all = Instant::now();
            for i in 0..iters {
                let t0 = Instant::now();
                let _g = wait_gpu(&mut cap, 5)?;
                let cap_ms = t0.elapsed().as_secs_f64() * 1000.0;
                let t1 = Instant::now();
                cap.copy_held_to_encoder_input()
                    .map_err(|e| format!("C copy to encoder input: {e}"))?;
                let aux_ms = t1.elapsed().as_secs_f64() * 1000.0;
                cap.release();
                let t2 = Instant::now();
                let pkt = enc
                    .encode_d3d11_registered(now_ms())
                    .map_err(|e| format!("C encode: {e}"))?;
                let enc_ms = t2.elapsed().as_secs_f64() * 1000.0;
                if rep.idr.is_empty() {
                    rep.idr = pkt.clone();
                    rep.idr_bytes = pkt.len();
                }
                rep.last_bytes = pkt.len();
                rep.frames += 1;
                rep.cap_ms += cap_ms;
                rep.enc_ms += enc_ms;
                rep.aux_ms += aux_ms;
                rep.max_cap = rep.max_cap.max(cap_ms);
                rep.max_enc = rep.max_enc.max(enc_ms);
                if i == 0 || i + 1 == iters {
                    println!(
                        "C frame {i}: capture {cap_ms:.2}ms gpu_blit {aux_ms:.2}ms encode {enc_ms:.2}ms {} bytes",
                        pkt.len()
                    );
                }
            }
            rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
            reports.push(("dxgi+NVENC(zero-copy)", rep));
        }

        // D: 直接把桌面纹理注册给 NVENC
        println!("\n--- D: DXGI + NVENC(direct register, 无 GPU 拷贝) ---");
        {
            let mut enc = NvencEncoder::new_d3d11(cfg, cap.device_raw())
                .map_err(|e| format!("D nvenc d3d11: {e}"))?;
            println!("D backend = {}", enc.input_backend_name());
            let mut rep = Report::new();
            let t_all = Instant::now();
            let mut failed: Option<String> = None;
            for i in 0..iters {
                let t0 = Instant::now();
                let g = wait_gpu(&mut cap, 5)?;
                let cap_ms = t0.elapsed().as_secs_f64() * 1000.0;
                let t1 = Instant::now();
                match enc.encode_d3d11_texture(g.texture, now_ms()) {
                    Ok(pkt) => {
                        let enc_ms = t1.elapsed().as_secs_f64() * 1000.0;
                        if rep.idr.is_empty() {
                            rep.idr = pkt.clone();
                            rep.idr_bytes = pkt.len();
                        }
                        rep.last_bytes = pkt.len();
                        rep.frames += 1;
                        rep.cap_ms += cap_ms;
                        rep.enc_ms += enc_ms;
                        rep.max_cap = rep.max_cap.max(cap_ms);
                        rep.max_enc = rep.max_enc.max(enc_ms);
                        if i == 0 || i + 1 == iters {
                            println!(
                                "D frame {i}: capture {cap_ms:.2}ms encode {enc_ms:.2}ms {} bytes",
                                pkt.len()
                            );
                        }
                    }
                    Err(e) => {
                        failed = Some(format!("{e}"));
                        cap.release();
                        break;
                    }
                }
                cap.release();
            }
            rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
            match failed {
                Some(e) => println!(
                    "D 不可用: {e}（该桌面纹理不能被 NVENC 直接注册，回落 C 的 GPU blit 路径）"
                ),
                None => reports.push(("dxgi+NVENC(direct)", rep)),
            }
        }
    }

    // ---------------- E: 单独验证 NVENC DIRECTX 输入路径 ----------------
    // 这条不依赖 Desktop Duplication：把 xcap 的一帧上传成 D3D11 纹理，
    // 再走「注册一次 -> 每帧直接编码」和「每帧注册再编码」两种方式。
    println!("\n--- E: NVENC(DirectX) 纹理直编（不依赖 DXGI 采集）---");
    let src = xcap_frame_data.ok_or("no xcap frame for E")?;
    let tex = cap
        .upload_bgra_texture(&src, cfg.width, cfg.height)
        .map_err(|e| format!("E upload_bgra_texture: {e}"))?;
    println!(
        "E texture = {tex:?} ({}x{} B8G8R8A8)",
        cfg.width, cfg.height
    );
    {
        let mut enc = NvencEncoder::new_d3d11(cfg, cap.device_raw())
            .map_err(|e| format!("E nvenc d3d11: {e}"))?;
        println!("E backend = {}", enc.input_backend_name());
        enc.register_d3d11_input(tex)
            .map_err(|e| format!("E register_d3d11_input: {e}"))?;
        let mut rep = Report::new();
        let t_all = Instant::now();
        for i in 0..iters {
            let t = Instant::now();
            let pkt = enc
                .encode_d3d11_registered(now_ms())
                .map_err(|e| format!("E encode: {e}"))?;
            let enc_ms = t.elapsed().as_secs_f64() * 1000.0;
            if rep.idr.is_empty() {
                rep.idr = pkt.clone();
                rep.idr_bytes = pkt.len();
            }
            rep.last_bytes = pkt.len();
            rep.frames += 1;
            rep.enc_ms += enc_ms;
            rep.max_enc = rep.max_enc.max(enc_ms);
            if i == 0 || i + 1 == iters {
                println!("E frame {i}: encode {enc_ms:.2}ms {} bytes", pkt.len());
            }
        }
        rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
        reports.push(("nvenc(directx, 常驻注册)", rep));
    }
    {
        let mut enc = NvencEncoder::new_d3d11(cfg, cap.device_raw())
            .map_err(|e| format!("E2 nvenc d3d11: {e}"))?;
        println!("E2 backend = {}", enc.input_backend_name());
        let mut rep = Report::new();
        let t_all = Instant::now();
        for i in 0..iters {
            let t = Instant::now();
            let pkt = enc
                .encode_d3d11_texture(tex, now_ms())
                .map_err(|e| format!("E2 encode: {e}"))?;
            let enc_ms = t.elapsed().as_secs_f64() * 1000.0;
            if rep.idr.is_empty() {
                rep.idr = pkt.clone();
                rep.idr_bytes = pkt.len();
            }
            rep.last_bytes = pkt.len();
            rep.frames += 1;
            rep.enc_ms += enc_ms;
            rep.max_enc = rep.max_enc.max(enc_ms);
            if i == 0 || i + 1 == iters {
                println!(
                    "E2 frame {i}: encode(含 register) {enc_ms:.2}ms {} bytes",
                    pkt.len()
                );
            }
        }
        rep.wall_ms = t_all.elapsed().as_secs_f64() * 1000.0;
        reports.push(("nvenc(directx, 每帧注册)", rep));
    }

    // ---------------- 汇总 ----------------
    println!(
        "\n=== 汇总（{} 帧/路径，{}x{}）===",
        iters, cfg.width, cfg.height
    );
    println!(
        "{:<28} {:>12} {:>12} {:>12} {:>10} {:>16} {:>8}",
        "路径", "capture_ms", "encode_ms", "aux_ms", "e2e_fps", "decode", "idr_B"
    );
    let mut all_ok = true;
    let mut lines = Vec::new();
    for (name, rep) in &reports {
        let dims = decode_dims(&rep.idr);
        let want = format!("{}x{}", cfg.width, cfg.height);
        let ok = dims == want;
        if !ok {
            all_ok = false;
        }
        let has_capture = rep.cap_ms > 0.0;
        println!(
            "{:<28} {:>12} {:>12.2} {:>12.2} {:>10} {:>16} {:>8}",
            name,
            if has_capture {
                format!("{:.2}", rep.avg_cap())
            } else {
                "n/a".into()
            },
            rep.avg_enc(),
            rep.avg_aux(),
            if has_capture {
                format!("{:.2}", rep.fps())
            } else {
                "n/a".into()
            },
            if ok {
                format!("{dims} OK")
            } else {
                format!("{dims} WANT {want}")
            },
            rep.idr_bytes
        );
        lines.push(format!(
            "{} capture_ms={} encode_ms={:.2} aux_ms={:.2} e2e_fps={} decode={} idr_bytes={} last_bytes={} max_capture_ms={:.2} max_encode_ms={:.2}",
            name,
            if has_capture { format!("{:.2}", rep.avg_cap()) } else { "n/a".into() },
            rep.avg_enc(),
            rep.avg_aux(),
            if has_capture { format!("{:.2}", rep.fps()) } else { "n/a".into() },
            dims,
            rep.idr_bytes,
            rep.last_bytes,
            rep.max_cap,
            rep.max_enc
        ));
    }

    let get = |prefix: &str| {
        reports
            .iter()
            .find(|(n, _)| n.starts_with(prefix))
            .map(|(_, r)| r.avg_cap())
    };
    let xcap_capture = get("xcap").unwrap_or(f64::NAN);
    let dxgi_capture = get("dxgi+NVENC(zero").unwrap_or(f64::NAN);
    println!(
        "\nCAPTURE_BUDGET_40MS: xcap_baseline={xcap_capture:.2}ms dxgi_zero_copy={}",
        if dxgi_capture.is_nan() {
            "n/a".into()
        } else {
            format!("{dxgi_capture:.2}ms")
        }
    );
    if let Some(reason) = &dxgi_blocked {
        println!("DXGI_BLOCKED_REASON: {reason}");
    }
    println!("RESULT: paths={} all_dims_ok={all_ok}", reports.len());
    for l in &lines {
        println!("METRIC {l}");
    }
    if !all_ok {
        println!(
            "VERDICT: FAIL (解码尺寸与 {}x{} 不一致)",
            cfg.width, cfg.height
        );
        return Err("decode dims mismatch".into());
    }
    if dxgi_blocked.is_some() {
        println!(
            "VERDICT: PARTIAL（NVENC DirectX 输入路径真机通过，DXGI 采集因桌面不出帧无法测量）"
        );
        return Ok(());
    }
    println!("VERDICT: PASS");
    Ok(())
}
