//! 跨机验证 · 被控端（Windows RTX 3060 机器）。
//!
//! 全链路：真实 gateway（登录/绑设备/登记公钥/建会话/取票据）→ 真实 rendezvous（WS 信令）
//! → 真实 relay（UDP 中继）→ 采集本机屏幕 → 编码（NVENC 零拷贝优先）→ 加密 → 中继；
//! 反向：收主控帧 → 解码；收主控输入事件 → SendInput 真注入 → 回读光标坐标。
//!
//! **必须在交互会话里跑**（Session 0 没有交互桌面：抓屏是 1024x768 假桌面、
//! SendInput 被系统静默拒绝、GetCursorPos 失败）。用 schtasks /IT 启动。
//!
//! 画面扰动：`scene_secs` 秒内起一个**自己会动的小窗口**（scene_move.ps1），
//! 全程不碰鼠标——上一轮用「绕光标画圈」制造变化，把注入精度测成了谜。
//! 发完帧立刻杀掉扰动进程，读一次光标基线，确认扰动真的停了，再注入、再回读。
//!
//! 用法：
//!   xmachine_target.exe <gateway> <relay_udp> <phone> <sms> <signal_base> \
//!                       <in_frames> <out_frames> <hello_file> <input_events> [scene_secs]

#[cfg(target_os = "windows")]
fn main() {
    real_main();
}

#[cfg(not(target_os = "windows"))]
fn main() {
    println!("xmachine_target 只能在 Windows 上跑（被控端注入需要 SendInput）");
}

#[cfg(target_os = "windows")]
fn real_main() {
    use std::net::SocketAddr;
    use std::time::{Duration, Instant};

    use er_capture::{Capture, ScreenCapture};
    use er_client::agent::TargetAgent;
    use er_input::windows::WindowsInjector;
    use er_input::{Injector, InputEvent};

    fn arg(i: usize, default: &str) -> String {
        std::env::args()
            .nth(i)
            .unwrap_or_else(|| default.to_string())
    }

    /// 起一个「自己会动」的窗口来制造画面变化。**不碰鼠标**。
    fn scene_start(seconds: u32) -> Option<std::process::Child> {
        if seconds == 0 {
            println!("WIN: scene=off（不制造画面变化）");
            return None;
        }
        let p = std::process::Command::new("powershell.exe")
            .args([
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                "C:\\EasyRemote\\scene_move.ps1",
                "-Seconds",
                &seconds.to_string(),
            ])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .ok();
        match &p {
            Some(c) => println!("WIN: scene=on pid={} seconds={seconds}", c.id()),
            None => println!("WIN: scene=on spawn_failed（扰动没起来，帧数可能只有 1）"),
        }
        p
    }

    fn scene_stop(child: Option<std::process::Child>) -> bool {
        match child {
            Some(mut c) => {
                let killed = c.kill().is_ok();
                let _ = c.wait();
                killed
            }
            None => false,
        }
    }

    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700005201");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let in_frames: usize = arg(6, "5").parse().expect("in_frames");
    let out_frames: usize = arg(7, "5").parse().expect("out_frames");
    let hello_file = arg(8, "win_hello.txt");
    let want_input: usize = arg(9, "3").parse().expect("input_events");
    let scene_secs: u32 = arg(10, "0").parse().expect("scene_secs");

    // 注入器先建：DPI 感知必须在任何 GetCursorPos 之前设好，否则回读坐标被虚拟化
    let mut injector = WindowsInjector::new(true);
    let screen = WindowsInjector::screen_size();

    println!("WIN: gateway={base} relay={relay_addr} signal={signal}");
    println!(
        "WIN: pid={} hardware_encoder_available={}",
        std::process::id(),
        er_codec::hardware_encoder_available()
    );
    let t_start = Instant::now();

    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    println!(
        "WIN: device_id={} device_code={} captcha={} ({}ms)",
        target.device_id(),
        code,
        captcha,
        t_start.elapsed().as_millis()
    );
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

    println!("WIN: waiting_for_signalling ...");
    let session_id = target
        .wait_for_session(&signal, Duration::from_secs(180))
        .expect("signalling");
    println!("WIN: session={session_id}");
    target.consent(&session_id).expect("consent");
    println!("WIN: consent=authorized");

    std::thread::sleep(Duration::from_millis(1200));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).expect("begin");
    println!("WIN: relay_registered local={}", relay.local_addr());
    target.announce(&mut relay, &msg).expect("announce");
    target
        .finish(&mut relay, &mut crypto, &session_id)
        .expect("handshake");
    println!("WIN: handshake_verified (ed25519 + x25519)");

    // ① 收主控发来的帧并解码
    let got = target
        .recv_frames(&mut relay, &mut crypto, in_frames)
        .expect("recv frames");
    println!(
        "WIN: frames_received={} resolution={}x{} first_h264_bytes={}",
        got.len(),
        got[0].width,
        got[0].height,
        got[0].h264_bytes
    );

    // ② 扰动开始 → 采集屏幕 → 编码（NVENC 零拷贝优先）→ 中继 → 扰动停止
    let cap_info = ScreenCapture::open_primary().map(|c| (c.info().width, c.info().height));
    match &cap_info {
        Ok((w, h)) => println!("WIN: capture_display={w}x{h}"),
        Err(e) => println!("WIN: capture_display_error={e}"),
    }
    let scene = scene_start(scene_secs);
    // 让窗口先真的画出来（首帧是 DXGI 变更的触发点）
    std::thread::sleep(Duration::from_millis(400));
    let t0 = Instant::now();
    let sizes = target
        .send_frames(&mut relay, &mut crypto, out_frames)
        .expect("send frames");
    let encode_ms = t0.elapsed().as_millis();
    let scene_stopped = scene_stop(scene);
    println!(
        "WIN: frames_sent={} bytes_total={} ms={} dims={}x{}",
        sizes.len(),
        sizes.iter().sum::<usize>(),
        encode_ms,
        cap_info.as_ref().map(|c| c.0 & !1).unwrap_or(0),
        cap_info.as_ref().map(|c| c.1 & !1).unwrap_or(0)
    );
    println!("WIN: scene_stopped={scene_stopped}");

    // ③ 扰动必须真的停了，基线才有意义：先等窗口消失 + 系统光标稳定，再读基线
    std::thread::sleep(Duration::from_millis(600));
    let baseline = WindowsInjector::cursor_position();
    let baseline2 = WindowsInjector::cursor_position();
    println!(
        "WIN: cursor_baseline={:?} cursor_baseline_recheck={:?} stable={}",
        baseline,
        baseline2,
        baseline == baseline2
    );

    // ④ 收主控输入事件并**真注入**，注入前后各读一次光标
    let before = WindowsInjector::cursor_position();
    println!(
        "WIN: screen={}x{} cursor_before={:?}",
        screen.0, screen.1, before
    );

    let injected: Vec<InputEvent> = target
        .recv_input(
            &mut relay,
            &mut crypto,
            &mut injector,
            want_input,
            Duration::from_secs(90),
        )
        .expect("recv input");
    // 注入后系统光标更新有短暂延迟
    std::thread::sleep(Duration::from_millis(250));
    let after = WindowsInjector::cursor_position();
    let moves: Vec<(i32, i32)> = injected
        .iter()
        .filter_map(|e| match e {
            InputEvent::MouseMove { x, y } => Some((*x, *y)),
            _ => None,
        })
        .collect();
    println!(
        "WIN: input_events_received={} kinds={:?}",
        injected.len(),
        injected
    );
    println!("WIN: injected_moves={:?}", moves);
    println!("WIN: cursor_after={:?}", after);

    let moved_ok = match (moves.last(), after) {
        (Some(&(tx, ty)), Some((ax, ay))) => (ax - tx).abs() <= 2 && (ay - ty).abs() <= 2,
        _ => false,
    };
    let delta = match (moves.last(), after) {
        (Some(&(tx, ty)), Some((ax, ay))) => (ax - tx, ay - ty),
        _ => (i32::MIN, i32::MIN),
    };
    // 扰动已停的硬证据：注入前两次基线读数一致
    let perturbation_stopped = baseline.is_some() && baseline == baseline2;
    println!(
        "WIN: moved_to_target={} (target={:?} actual={:?} delta={:?})",
        moved_ok,
        moves.last(),
        after,
        delta
    );
    println!("WIN: perturbation_stopped={perturbation_stopped}");
    println!(
        "WIN: RESULT frames_received={} frames_sent={} input_events={} moved_to_target={} perturbation_stopped={} elapsed_ms={}",
        got.len(),
        sizes.len(),
        injected.len(),
        moved_ok,
        perturbation_stopped,
        t_start.elapsed().as_millis()
    );
    println!("WIN: DONE session={session_id}");
    if got.is_empty() || sizes.is_empty() || !moved_ok || !perturbation_stopped {
        println!("WIN: VERDICT FAIL");
        std::process::exit(1);
    }
    println!("WIN: VERDICT PASS");
}
