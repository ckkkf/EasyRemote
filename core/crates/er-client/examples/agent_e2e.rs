//! 单端代理端到端：两个独立代理只通过服务端交互。
//!
//! 关键顺序：两端都先在中继注册，再交换握手消息——
//! 否则先发的包会因为对端未注册而被中继丢弃（这一步踩过）。

use std::net::SocketAddr;
use std::sync::mpsc;
use std::thread;

use er_client::agent::{ControllerAgent, TargetAgent};

fn main() {
    let base = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "http://127.0.0.1:8080".into());
    let relay_addr: SocketAddr = std::env::args()
        .nth(2)
        .unwrap_or_else(|| "127.0.0.1:19121".into())
        .parse()
        .expect("relay addr");

    let target =
        TargetAgent::start(&base, "13700004001", "123456", relay_addr).expect("target start");
    let ctrl =
        ControllerAgent::start(&base, "13700004002", "123456", relay_addr).expect("ctrl start");
    println!("target={} ctrl={}", target.device_id(), ctrl.device_id());

    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    let session_id = ctrl
        .request_session(&code, &captcha)
        .expect("request session");
    println!("session={session_id}");
    target.consent(&session_id).expect("consent");

    // 1) 两端都注册（不发送）
    let (mut c_relay, mut c_crypto, c_keys, c_msg) = ctrl.begin(&session_id).expect("ctrl begin");
    let (mut t_relay, mut t_crypto, t_msg) = target.begin(&session_id).expect("target begin");
    println!("both sides registered at relay");

    // 2) 交换握手
    ctrl.announce(&mut c_relay, &c_msg).expect("ctrl announce");
    target
        .announce(&mut t_relay, &t_msg)
        .expect("target announce");

    // 3) 各自验签
    target
        .finish(&mut t_relay, &mut t_crypto, &session_id)
        .expect("target finish");
    ctrl.finish(&mut c_relay, &mut c_crypto, &c_keys, &session_id)
        .expect("ctrl finish");
    println!("both handshakes verified against server-provided keys");

    // 4) 发帧 / 收帧
    let (tx, rx) = mpsc::channel::<usize>();
    let sender = thread::spawn(move || {
        let sizes = ctrl
            .send_frames(&mut c_relay, &mut c_crypto, 5)
            .expect("send frames");
        tx.send(sizes.len()).expect("signal");
        sizes
    });

    let frames = target
        .recv_frames(&mut t_relay, &mut t_crypto, 5)
        .expect("recv frames");
    let notified = rx.recv().expect("send count");
    let sent = sender.join().expect("sender thread");

    println!(
        "sent {} frames, received {} frames",
        sent.len(),
        frames.len()
    );
    for (i, f) in frames.iter().enumerate() {
        println!("frame {i}: {}x{} {} bytes", f.width, f.height, f.h264_bytes);
    }

    if frames.len() != sent.len() || notified != sent.len() {
        println!(
            "FAIL: frame count mismatch ({}/{})",
            frames.len(),
            sent.len()
        );
        std::process::exit(1);
    }
    if frames.iter().any(|f| f.width == 0 || f.height == 0) {
        println!("FAIL: bad frame dims");
        std::process::exit(1);
    }
    println!(
        "AGENT E2E PASSED ({} frames via independent single-end agents)",
        frames.len()
    );
}
