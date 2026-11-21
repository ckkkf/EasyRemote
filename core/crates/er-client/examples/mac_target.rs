//! 跨机验证 · 被控端（macOS 本机）。
//!
//! 全链路：真实 gateway（登录/绑设备/登记公钥/取票据）→ 真实 rendezvous（WS 信令）
//! → 真实 relay（UDP 中继）→ 采集本机屏 → 编码 → 加密 → 中继；
//! 反向：收主控帧 → 解码；收主控输入事件 → CGEventPost 真注入 → **按像素**回读光标。
//!
//! 与 Windows 被控端（xmachine_target）的差异，全部来自 macOS 的特性：
//!   1. 采集走 xcap/CoreGraphics，静止桌面每帧都能出（没有 DXGI 的"无变化不出帧"），
//!      所以不需要 PowerShell 小窗扰动，也不需要 perturbation_stopped 断言。
//!   2. 采集帧是**像素**，CGEvent 坐标是 **points**（Retina 差 2 倍）。
//!      注入器负责换算，这里按像素坐标下发并**按像素回读**，才不会自欺欺人。
//!   3. 需要两项系统授权：屏幕录制（采集）+ 辅助功能（注入）。缺任一项都会表现为
//!      "链路通但画面黑/光标不动"，所以本程序先自检、把结论打进日志，再断言。
//!
//! 必须在**有 WindowServer 的登录会话**里跑：SSH 后台会话没有 Aqua 会话，
//! 采集会拿到空帧、CGEventPost 会被丢弃。由 scripts/mac/step4_win2mac.sh 从桌面会话拉起。
//!
//! 用法：
//!   mac_target <gateway> <relay_udp> <phone> <sms> <signal_base> <in_frames> <out_frames> <hello_file> <input_events>

#[cfg(target_os = "macos")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "macos"))]
fn main() {
    println!("mac_target 只能在 macOS 上跑（被控端注入需要 CGEventPost）");
}

#[cfg(target_os = "macos")]
fn real_main() {
    use std::net::SocketAddr;
    use std::time::{Duration, Instant};

    use er_capture::{Capture, ScreenCapture};
    use er_client::agent::TargetAgent;
    use er_input::macos::{MacDisplay, MacInjector};
    use er_input::InputEvent;

    fn arg(i: usize, default: &str) -> String {
        std::env::args()
            .nth(i)
            .unwrap_or_else(|| default.to_string())
    }

    /// 采集帧是否"有画面"：全黑/全零 = 没拿到真实屏幕（屏录权限被拒的典型表现）。
    fn frame_has_content(data: &[u8]) -> (bool, u64) {
        // 抽样：每 997 字节取一个，避免全量扫描拖慢
        let mut nonzero = 0u64;
        let mut total = 0u64;
        for b in data.iter().step_by(997) {
            total += 1;
            if *b != 0 {
                nonzero += 1;
            }
        }
        let ratio = if total == 0 {
            0.0
        } else {
            nonzero as f64 / total as f64
        };
        (ratio > 0.01, nonzero)
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700008101");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let in_frames: usize = arg(6, "5").parse().expect("in_frames");
    let out_frames: usize = arg(7, "5").parse().expect("out_frames");
    let hello_file = arg(8, "mac_hello.txt");
    let want_input: usize = arg(9, "3").parse().expect("input_events");
    // 演示/长会话用：注入阶段的总预算（默认 120s 适合 3 个事件，长演示要放大）
    let input_timeout: u64 = arg(10, "120").parse().unwrap_or(120);
    // 演示用：1 = 先注入再发帧（让主控画圆结束后才录屏，画面里能看到光标停点）
    let send_after_input: bool = arg(11, "0") == "1";

    let t_start = Instant::now();
    let disp = MacDisplay::main();
    println!(
        "MAC: gateway={base} relay={relay_addr} signal={signal} pid={}",
        std::process::id()
    );
    println!(
        "MAC: display pixels={}x{} points={}x{} origin=({},{}) scale={}",
        disp.pixel_width,
        disp.pixel_height,
        disp.point_width,
        disp.point_height,
        disp.origin_x,
        disp.origin_y,
        disp.scale()
    );

    // ---------- ① 授权自检（必须在开会话之前：它决定后面断言是否可信） ----------
    let (screen_ok, screen_detail) = match ScreenCapture::open_primary() {
        Ok(mut cap) => {
            let info = cap.info();
            match cap.grab() {
                Ok(f) => {
                    let (has_content, nz) = frame_has_content(&f.data);
                    (
                        has_content,
                        format!(
                            "info={}x{} frame={}x{} nonzero_samples={nz}",
                            info.width, info.height, f.width, f.height
                        ),
                    )
                }
                Err(e) => (false, format!("grab failed: {e}")),
            }
        }
        Err(e) => (false, format!("open_primary failed: {e}")),
    };
    println!(
        "MAC: permission_screen={} ({screen_detail})",
        if screen_ok { "ok" } else { "denied" }
    );

    let acc_baseline = MacInjector::cursor_pixel();
    println!(
        "MAC: cursor_baseline_pixel={:?} (读不到 = 没有输入监控/辅助功能会话)",
        acc_baseline
    );

    // ---------- ② 开会话 ----------
    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    println!(
        "MAC: device_id={} device_code={} captcha={} ({}ms)",
        target.device_id(),
        code,
        captcha,
        t_start.elapsed().as_millis()
    );
    // 真实产品里这两个数字显示在被控端屏幕上；跨机编排靠文件传递
    std::fs::write(
        &hello_file,
        format!(
            "device_id={}\ndevice_code={}\ncaptcha={}\n",
            target.device_id(),
            code,
            captcha
        ),
    )
    .expect("write hello");

    println!("MAC: waiting_for_signalling ...");
    let session_id = target
        .wait_for_session(&signal, Duration::from_secs(240))
        .expect("signalling");
    println!("MAC: session={session_id}");
    target.consent(&session_id).expect("consent");
    println!("MAC: consent=authorized");

    std::thread::sleep(Duration::from_millis(1200));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).expect("begin");
    println!("MAC: relay_registered local={}", relay.local_addr());
    // 中继只转发「两端都已注册」的包：主控在 request_session 之后还要 sleep 1.5s 才注册，
    // 被控端抢跑会把握手发进真空，然后双方各等 15s 超时（本机回环实测踩到过）。
    // 这里等到 2.5s（与主控 announce 的时序对称），把「谁先到」的竞态消掉。
    std::thread::sleep(Duration::from_millis(2500));
    target.announce(&mut relay, &msg).expect("announce");
    target
        .finish(&mut relay, &mut crypto, &session_id)
        .expect("handshake");
    println!("MAC: handshake_verified (ed25519 + x25519)");

    // ---------- ③ 收主控发来的帧并解码 ----------
    // 收不到帧也不 panic：打印 0 帧，让 VERDICT 判 FAIL，日志可断言（便于跨机排障）
    let got = target
        .recv_frames(&mut relay, &mut crypto, in_frames)
        .unwrap_or_default();
    if got.is_empty() {
        println!("MAC: frames_received=0 (主控侧没有发出可解码的帧)");
    } else {
        println!(
            "MAC: frames_received={} resolution={}x{} first_h264_bytes={}",
            got.len(),
            got[0].width,
            got[0].height,
            got[0].h264_bytes
        );
    }

    // ---------- ④ 采集本机真实屏幕并发给主控 ----------
    // 演示模式（send_after_input）把这一步推迟到注入之后：
    // 主控的输入演示先跑，用户能现场看到 X 光标在动；随后才编码回传，
    // 这样录下来的画面里光标停在演示的终点，可核对。
    let mut sizes: Vec<usize> = Vec::new();
    if !send_after_input {
        let t0 = Instant::now();
        sizes = target
            .send_frames(&mut relay, &mut crypto, out_frames)
            .expect("send frames");
        println!(
            "MAC: frames_sent={} bytes_total={} ms={}",
            sizes.len(),
            sizes.iter().sum::<usize>(),
            t0.elapsed().as_millis()
        );
    }

    // ---------- ⑤ 注入前光标基线（两次读数一致才算稳定） ----------
    std::thread::sleep(Duration::from_millis(400));
    let baseline = MacInjector::cursor_pixel();
    let baseline2 = MacInjector::cursor_pixel();
    println!(
        "MAC: cursor_baseline={:?} recheck={:?} stable={}",
        baseline,
        baseline2,
        baseline == baseline2
    );

    // ---------- ⑥ 收主控输入并真注入 ----------
    let mut injector = MacInjector::new(true);
    // 收不满不算失败（UDP 会丢个别事件）：超时后返回已收到的，moved_to_target 仍可断言
    let injected: Vec<InputEvent> = target
        .recv_input(
            &mut relay,
            &mut crypto,
            &mut injector,
            want_input,
            Duration::from_secs(input_timeout),
        )
        .unwrap_or_default();
    if injected.is_empty() {
        println!("MAC: input_events_received=0 (主控没有发出可注入的输入)");
    }
    std::thread::sleep(Duration::from_millis(250));
    let after = MacInjector::cursor_pixel();

    let moves: Vec<(i32, i32)> = injected
        .iter()
        .filter_map(|e| match e {
            InputEvent::MouseMove { x, y } => Some((*x, *y)),
            _ => None,
        })
        .collect();
    println!(
        "MAC: input_events_received={} kinds={:?}",
        injected.len(),
        injected
    );
    println!("MAC: injected_moves={:?}", moves);
    println!("MAC: injected_mapped={}", injector.mapped_moves());
    println!("MAC: cursor_after={:?}", after);

    let moved_ok = match (moves.last(), after) {
        (Some(&(tx, ty)), Some((ax, ay))) => (ax - tx).abs() <= 2 && (ay - ty).abs() <= 2,
        _ => false,
    };
    let delta = match (moves.last(), after) {
        (Some(&(tx, ty)), Some((ax, ay))) => (ax - tx, ay - ty),
        _ => (i32::MIN, i32::MIN),
    };
    println!(
        "MAC: moved_to_target={} (target={:?} actual={:?} delta={:?})",
        moved_ok,
        moves.last(),
        after,
        delta
    );
    println!(
        "MAC: cursor_stable_before_inject={}",
        baseline.is_some() && baseline == baseline2
    );

    // ---------- ⑦ 演示模式：注入之后才编码回传（主控正在录像） ----------
    if send_after_input {
        let t0 = Instant::now();
        sizes = target
            .send_frames(&mut relay, &mut crypto, out_frames)
            .unwrap_or_default();
        println!(
            "MAC: frames_sent={} bytes_total={} ms={} (after_input=true)",
            sizes.len(),
            sizes.iter().sum::<usize>(),
            t0.elapsed().as_millis()
        );
    }

    println!(
        "MAC: RESULT screen_permission={} frames_received={} frames_sent={} input_events={} moved_to_target={} elapsed_ms={}",
        screen_ok,
        got.len(),
        sizes.len(),
        injected.len(),
        moved_ok,
        t_start.elapsed().as_millis()
    );
    println!("MAC: DONE session={session_id}");

    if !screen_ok || got.is_empty() || sizes.is_empty() || !moved_ok {
        println!("MAC: VERDICT FAIL");
        std::process::exit(1);
    }
    println!("MAC: VERDICT PASS");
}
