//! 两台「机器」的信令流程：主控发起 → 被控从信令收到请求 → 授权 → 传帧。
//!
//! 与之前的例子不同：被控端不再由代码告知 session_id，
//! 而是自己连 rendezvous 的 WebSocket 收到 CONSENT_REQUIRED。

use std::net::SocketAddr;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use er_client::agent::{ControllerAgent, TargetAgent};

fn main() {
    let base = std::env::args().nth(1).unwrap_or_else(|| "http://127.0.0.1:18099".into());
    // 信令与网关是两个独立服务：HTTP 走网关，WebSocket 走 rendezvous
    let signal_base = std::env::args().nth(3).unwrap_or_else(|| "http://127.0.0.1:18098".into());
    let relay_addr: SocketAddr = std::env::args()
        .nth(2)
        .unwrap_or_else(|| "127.0.0.1:19121".into())
        .parse()
        .expect("relay addr");

    let target =
        TargetAgent::start(&base, "13700030001", "123456", relay_addr).expect("target start");
    let ctrl =
        ControllerAgent::start(&base, "13700030002", "123456", relay_addr).expect("ctrl start");
    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    println!("target={} code={}", target.device_id(), code);

    // 被控端在后台等信令通知
    let (tx, rx) = mpsc::channel::<String>();
    let signal_base_clone = signal_base.clone();
    let target_for_thread = thread::spawn(move || {
        let sid = target.wait_for_session(&signal_base_clone, Duration::from_secs(10)).expect("signalling session");
        tx.send(sid.clone()).expect("send sid");
        (target, sid)
    });

    // 主控发起（此时被控端的 WS 已经连上）
    thread::sleep(Duration::from_millis(600));
    let session_id = ctrl.request_session(&code, &captcha).expect("request session");
    println!("controller created session={session_id}");

    let signalled = rx.recv().expect("recv sid");
    println!("target learned session via signalling: {signalled}");
    if signalled != session_id {
        println!("FAIL: signalling delivered a different session id");
        std::process::exit(1);
    }

    let (target, _sid) = target_for_thread.join().expect("target thread");

    // 授权 + 双端握手 + 传帧
    target.consent(&session_id).expect("consent");
    let (mut c_relay, mut c_crypto, c_keys, c_msg) = ctrl.begin(&session_id).expect("ctrl begin");
    let (mut t_relay, mut t_crypto, t_msg) = target.begin(&session_id).expect("target begin");
    ctrl.announce(&mut c_relay, &c_msg).expect("ctrl announce");
    target.announce(&mut t_relay, &t_msg).expect("target announce");
    target.finish(&mut t_relay, &mut t_crypto, &session_id).expect("target finish");
    ctrl.finish(&mut c_relay, &mut c_crypto, &c_keys, &session_id).expect("ctrl finish");

    let (ftx, frx) = mpsc::channel::<usize>();
    let sender = thread::spawn(move || {
        let sizes = ctrl.send_frames(&mut c_relay, &mut c_crypto, 5).expect("send frames");
        ftx.send(sizes.len()).expect("signal");
        sizes
    });
    let frames = target.recv_frames(&mut t_relay, &mut t_crypto, 5).expect("recv frames");
    let _ = frx.recv();
    let sent = sender.join().expect("sender");

    println!("sent {} frames, received {} frames", sent.len(), frames.len());
    for (i, f) in frames.iter().enumerate() {
        println!("frame {i}: {}x{} {} bytes", f.width, f.height, f.h264_bytes);
    }
    if frames.len() != sent.len() {
        println!("FAIL: frame count mismatch");
        std::process::exit(1);
    }
    println!("SIGNALLING E2E PASSED (target discovered session via WebSocket)");
}
