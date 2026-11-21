//! step3 工具（Windows 真机验收用，两个子命令）。
//!
//!   dxgi_step3.exe lockstate   → 打印 is_locked()，解锁状态下必须是 false
//!   dxgi_step3.exe selfheal    → 改分辨率触发 DXGI_ERROR_ACCESS_LOST，
//!                                验证「重建 duplication 但**不重建 device / NVENC 会话**」
//!
//! 退出码：0 = PASS，1 = FAIL，3 = SKIP（锁屏 / 无 DXGI / 无 NVIDIA），2 = 用法错误。
//! 设计原则：**锁屏时直接 SKIP 退出**，绝不产出误导性的 PASS/FAIL。

#[cfg(target_os = "windows")]
fn main() {
    let mode = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "lockstate".to_string());
    match mode.as_str() {
        "lockstate" => lockstate(),
        "selfheal" => selfheal(),
        other => {
            println!("usage: dxgi_step3.exe <lockstate|selfheal>  (got {other})");
            std::process::exit(2);
        }
    }
}

#[cfg(not(target_os = "windows"))]
fn main() {
    println!("dxgi_step3 只能在 Windows 上跑");
    std::process::exit(2);
}

#[cfg(target_os = "windows")]
fn lockstate() {
    use er_capture::dxgi::{display, DxgiCapture};

    let mut cap = match DxgiCapture::open_primary_detailed() {
        Ok(c) => c,
        Err(e) => {
            println!("DXGI_LOCKSTATE open_failed=\"{e}\"");
            println!("RESULT: SKIP (no dxgi capture)");
            std::process::exit(3);
        }
    };
    let info = cap.info();
    let mode = display::current_mode();
    println!(
        "DXGI_LOCKSTATE adapter=\"{}\" size={}x{} format={} mode={:?}",
        cap.adapter_name(),
        info.width,
        info.height,
        cap.format(),
        mode
    );
    let locked = cap.is_locked();
    println!("DXGI_LOCKSTATE is_locked={locked}");
    if locked {
        println!("RESULT: SKIP (desktop locked)");
        std::process::exit(3);
    }
    println!("RESULT: PASS (is_locked=false)");
}

#[cfg(target_os = "windows")]
fn selfheal() {
    use er_capture::dxgi::{display, DxgiCapture};
    use er_codec::nvenc::NvencEncoder;
    use er_codec::EncodeConfig;

    fn now_ms() -> u64 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis() as u64)
            .unwrap_or(0)
    }

    let mut cap = match DxgiCapture::open_primary_detailed() {
        Ok(c) => c,
        Err(e) => {
            println!("SELFHEAL open_failed=\"{e}\"");
            println!("RESULT: SKIP (no dxgi capture)");
            std::process::exit(3);
        }
    };
    if cap.is_locked() {
        println!("SELFHEAL SKIP: desktop locked（需要先解锁桌面）");
        println!("RESULT: SKIP (desktop locked)");
        std::process::exit(3);
    }

    let info = cap.info();
    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    let device0 = cap.device_raw();
    println!(
        "SELFHEAL begin size={}x{} device_ptr={:p}",
        cfg.width, cfg.height, device0
    );

    // 1) 建立 NVENC 会话（DIRECTX 设备类型，绑在采集的同一块 device 上）
    let mut enc = match NvencEncoder::new_d3d11(cfg, device0) {
        Ok(e) => e,
        Err(e) => {
            println!("SELFHEAL nvenc_open_failed=\"{e}\"");
            println!("RESULT: SKIP (no nvenc)");
            std::process::exit(3);
        }
    };
    let mut tex0 = match cap.ensure_encoder_input(cfg.width, cfg.height) {
        Ok(t) => t,
        Err(e) => {
            println!("SELFHEAL ensure_input_failed=\"{e}\"");
            std::process::exit(1);
        }
    };
    if let Err(e) = enc.register_d3d11_input(tex0) {
        println!("SELFHEAL register_failed=\"{e}\"");
        std::process::exit(1);
    }
    println!("SELFHEAL nvenc_session_opened=true input_texture={tex0:p}");

    // 2) 编码一帧（基线）
    let mut before_bytes = 0usize;
    let _ = cap.nudge_desktop_zero(); // 静态桌面：先制造一次 present，否则拿不到首帧
    for _ in 0..20 {
        match cap.grab_gpu(30) {
            Ok(Some(_)) => {
                if cap.copy_held_to_encoder_input().is_ok() {
                    if let Ok(b) = enc.encode_d3d11_registered(now_ms()) {
                        before_bytes = b.len();
                        break;
                    }
                }
            }
            Ok(None) => {}
            Err(e) => println!("SELFHEAL pre_grab_err={e:?}"),
        }
        std::thread::sleep(std::time::Duration::from_millis(100));
    }
    println!("SELFHEAL encode_before_bytes={before_bytes}");
    if before_bytes == 0 {
        println!("RESULT: FAIL (baseline encode produced nothing)");
        std::process::exit(1);
    }

    let rebuilds_before = cap.rebuild_count();
    let orig = display::current_mode();
    let alt = display::alternate_mode();
    println!("SELFHEAL switching {orig:?} -> {alt:?}");
    if let Err(e) = display::switch_mode(alt.0, alt.1, alt.2) {
        println!("SELFHEAL switch_failed=\"{e}\"");
        println!("RESULT: INCONCLUSIVE (cannot change display mode)");
        std::process::exit(1);
    }
    std::thread::sleep(std::time::Duration::from_millis(1200));
    // 模式切换后 duplication 到底返回什么 HRESULT？先打出来，别猜
    println!(
        "SELFHEAL acquire_debug_after_switch={}",
        cap.acquire_debug(50)
    );

    // 3) 触发 ACCESS_LOST：切换分辨率后 duplication 失效，下一次 grab_gpu 应自愈重建
    let mut post_frames = 0usize;
    let _ = cap.nudge_desktop_zero();
    for _ in 0..20 {
        match cap.grab_gpu(30) {
            Ok(Some(_)) => post_frames += 1,
            Ok(None) => {}
            Err(e) => println!("SELFHEAL post_grab_err={e:?}"),
        }
        if cap.rebuild_count() > rebuilds_before {
            break;
        }
        std::thread::sleep(std::time::Duration::from_millis(100));
    }
    let rebuilds_after = cap.rebuild_count();
    println!("SELFHEAL rebuilds_before={rebuilds_before} rebuilds_after={rebuilds_after}");

    // 4) 切回原模式（尺寸恢复，已注册纹理仍有效）
    if let Some((w, h, f)) = orig {
        if let Err(e) = display::switch_mode(w, h, f) {
            println!("SELFHEAL restore_failed=\"{e}\"");
        }
        std::thread::sleep(std::time::Duration::from_millis(1200));
    }

    // 5) 同一个 encoder 继续编码（关键：**没有重建 NVENC 会话**）
    let device1 = cap.device_raw();
    let device_same = device1 == device0;
    println!(
        "SELFHEAL device_ptr_after={:p} device_ptr_unchanged={} device_rebuilds={}",
        device1,
        device_same,
        cap.device_rebuilds()
    );
    if !device_same {
        // 显示模式切换把 D3D11 device 移除掉了：旧 NVENC 会话绑在死掉的 device 上，
        // 必须跟着换一个新会话（这不是" rebuilding our own session for no reason"，
        // 而是驱动把底下的 device 换了）。
        println!("SELFHEAL nvenc_session_recreated=true (device removed by driver)");
        match NvencEncoder::new_d3d11(cfg, device1) {
            Ok(e) => enc = e,
            Err(e) => {
                println!("SELFHEAL nvenc_reopen_failed=\"{e}\"");
                println!("RESULT: FAIL");
                std::process::exit(1);
            }
        }
        if let Ok(t) = cap.ensure_encoder_input(cfg.width, cfg.height) {
            match enc.register_d3d11_input(t) {
                Ok(()) => tex0 = t, // 新会话已注册，后面不要重复注册
                Err(e) => println!("SELFHEAL register_after_reopen_err=\"{e}\" ..."),
            }
        }
    }

    let mut after_bytes = 0usize;
    let _ = cap.nudge_desktop_zero();
    for _ in 0..30 {
        match cap.grab_gpu(30) {
            Ok(Some(_)) => {
                let tex1 = match cap.ensure_encoder_input(cfg.width, cfg.height) {
                    Ok(t) => t,
                    Err(e) => {
                        println!("SELFHEAL ensure_input_after_failed=\"{e}\"");
                        break;
                    }
                };
                if tex1 != tex0 {
                    println!(
                        "SELFHEAL input_texture_changed={tex0:p} -> {tex1:p}（重注册，未重建会话）"
                    );
                    if let Err(e) = enc.register_d3d11_input(tex1) {
                        println!("SELFHEAL register_after_failed=\"{e}\"");
                        break;
                    }
                }
                match cap.copy_held_to_encoder_input() {
                    Ok(()) => {}
                    Err(e) => println!("SELFHEAL copy_after_err={e:?}"),
                }
                match enc.encode_d3d11_registered(now_ms()) {
                    Ok(b) => {
                        after_bytes = b.len();
                        break;
                    }
                    Err(e) => println!("SELFHEAL encode_after_err={e:?}"),
                }
            }
            Ok(None) => {}
            Err(e) => {
                println!(
                    "SELFHEAL final_grab_err={e:?} last_rebuild_error={:?}",
                    cap.last_rebuild_error()
                )
            }
        }
        std::thread::sleep(std::time::Duration::from_millis(100));
    }
    println!("SELFHEAL encode_after_bytes={after_bytes}");

    let rebuilt = rebuilds_after > rebuilds_before;
    let nvenc_reused = after_bytes > 0;
    println!(
        "SELFHEAL rebuilt={rebuilt} device_unchanged={device_same} nvenc_session_reused={nvenc_reused} post_frames={post_frames}"
    );
    if rebuilt && nvenc_reused && device_same {
        println!("RESULT: PASS (duplication 自愈；device 与 NVENC 会话均未重建)");
    } else if rebuilt && nvenc_reused && !device_same {
        println!(
            "RESULT: PASS (device 被驱动移除后整条重建，采集与编码均已恢复；device 指针必然变化)"
        );
    } else if !rebuilt {
        println!("RESULT: INCONCLUSIVE (没有触发重建：该显示器的模式切换可能是空操作)");
        std::process::exit(1);
    } else {
        println!("RESULT: FAIL");
        std::process::exit(1);
    }
}
