//! Windows 被控端实跑：信令 → 授权 → 握手 → 解码主控帧 → 采集本机屏幕 → 编码 → 中继。
//!
//! 用法（Windows，必须在交互会话里跑，Session 0 抓不到屏）：
//!   win_target.exe <gateway> <relay_addr> <phone> <sms> <signal_base> <in_frames> <out_frames> <hello_file>
//!
//! 设备码/验证码由服务端生成，主控需要它们才能发起连接：本程序把它们写进
//! hello_file，主控侧读文件后发起。

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_client::agent::TargetAgent;

fn arg(i: usize, default: &str) -> String {
    std::env::args().nth(i).unwrap_or_else(|| default.to_string())
}

fn main() {
    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr =
        arg(2, "127.0.0.1:19000").parse().expect("relay addr");
    let phone = arg(3, "13700004101");
    let sms = arg(4, "123456");
    let signal = arg(5, &base.clone());
    let in_frames: usize = arg(6, "5").parse().expect("in_frames");
    let out_frames: usize = arg(7, "5").parse().expect("out_frames");
    let hello_file = arg(8, "win_hello.txt");

    println!("WIN: gateway={base} relay={relay_addr} signal={signal}");
    let target = TargetAgent::start(&base, &phone, &sms, relay_addr).expect("target start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    println!("WIN: device_id={} device_code={} captcha={}", target.device_id(), code, captcha);
    std::fs::write(
        &hello_file,
        format!("device_id={}\ndevice_code={}\ncaptcha={}\n", target.device_id(), code, captcha),
    )
    .expect("write hello");

    println!("WIN: waiting for signalling ...");
    let session_id = target
        .wait_for_session(&signal, Duration::from_secs(180))
        .expect("signalling session");
    println!("WIN: session={session_id}");
    target.consent(&session_id).expect("consent");
    println!("WIN: consent=authorized");

    // 先让主控在中继上注册好，再发本方握手
    std::thread::sleep(Duration::from_millis(1000));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).expect("begin");
    println!("WIN: relay registered");
    target.announce(&mut relay, &msg).expect("announce target handshake");
    target.finish(&mut relay, &mut crypto, &session_id).expect("handshake verify");
    println!("WIN: handshake verified (ed25519 + x25519)");

    // ① 解码主控发来的真实帧
    let got = target.recv_frames(&mut relay, &mut crypto, in_frames).expect("recv frames");
    println!(
        "WIN: frames_received={} resolution={}x{} first_h264_bytes={}",
        got.len(),
        got[0].width,
        got[0].height,
        got[0].h264_bytes
    );

    // ② 采集本机屏幕 → 编码 → 中继
    let info = ScreenCapture::open_primary()
        .map(|c| (c.info().width, c.info().height))
        .expect("open primary display");
    println!("WIN: capture_display={}x{}", info.0, info.1);
    let t0 = std::time::Instant::now();
    let sizes = target.send_frames(&mut relay, &mut crypto, out_frames).expect("send frames");
    let total: usize = sizes.iter().sum();
    println!(
        "WIN: frames_sent={} bytes_total={} ms={} dims={}x{}",
        sizes.len(),
        total,
        t0.elapsed().as_millis(),
        info.0 & !1,
        info.1 & !1
    );
    std::thread::sleep(Duration::from_millis(1500));
    println!("WIN: DONE session={session_id}");
}
