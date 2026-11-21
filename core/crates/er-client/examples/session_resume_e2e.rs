//! 会话级重连：中继重启后**不重建会话**，继续用同一套密钥收发。
//!
//! 与上一轮的链路重连不同，这里验证的是：
//!   加密会话（nonce 序列）与 session_id 跨中继重启保持有效。

use std::net::SocketAddr;
use std::process::{Child, Command, Stdio};
use std::time::Duration;

use er_client::agent::{ControllerAgent, TargetAgent};
use er_session::backoff::{retry_until, BackoffPolicy};

const RELAY_BIN: &str = "/tmp/er-relay-full";

fn spawn_relay(port: u16) -> Child {
    Command::new(RELAY_BIN)
        .args([
            "-addr",
            &format!(":{port}"),
            "-node-addr",
            &format!("127.0.0.1:{port}"),
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .expect("spawn relay")
}

fn main() {
    let base = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "http://127.0.0.1:18099".into());
    let port = 19141u16;
    let relay_addr: SocketAddr = format!("127.0.0.1:{port}").parse().unwrap();

    let mut relay_child = spawn_relay(port);
    std::thread::sleep(Duration::from_millis(700));

    let target = TargetAgent::start(&base, "13700070001", "123456", relay_addr).expect("target");
    let ctrl = ControllerAgent::start(&base, "13700070002", "123456", relay_addr).expect("ctrl");

    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    let session_id = ctrl.request_session(&code, &captcha).expect("session");
    target.consent(&session_id).expect("consent");

    let (mut c_relay, mut c_crypto, c_keys, c_msg) = ctrl.begin(&session_id).expect("c begin");
    let (mut t_relay, mut t_crypto, t_msg) = target.begin(&session_id).expect("t begin");
    ctrl.announce(&mut c_relay, &c_msg).expect("c announce");
    target.announce(&mut t_relay, &t_msg).expect("t announce");
    target
        .finish(&mut t_relay, &mut t_crypto, &session_id)
        .expect("t finish");
    ctrl.finish(&mut c_relay, &mut c_crypto, &c_keys, &session_id)
        .expect("c finish");
    println!("session {session_id} established");

    // 中断前先收 2 帧
    let before = {
        let n = ctrl
            .send_frames(&mut c_relay, &mut c_crypto, 2)
            .expect("send");
        let frames = target
            .recv_frames(&mut t_relay, &mut t_crypto, 2)
            .expect("recv");
        (n.len(), frames.len())
    };
    println!("before restart: sent {} received {}", before.0, before.1);
    if before.1 != 2 {
        println!("FAIL: baseline frames not delivered");
        let _ = relay_child.kill();
        std::process::exit(1);
    }

    // 杀掉中继
    let _ = relay_child.kill();
    let _ = relay_child.wait();
    std::thread::sleep(Duration::from_millis(300));

    // 重启中继，两端用**同一会话**重新注册（不重做握手、不换密钥）
    let mut relay_child = spawn_relay(port);
    std::thread::sleep(Duration::from_millis(700));

    let policy = BackoffPolicy {
        base_ms: 200,
        factor: 2,
        max_ms: 2000,
        max_attempts: 15,
    };
    let (_, retries) = retry_until(policy, Duration::from_secs(20), || {
        ctrl.rejoin(&mut c_relay, &session_id)
            .map_err(|e| e.to_string())?;
        target
            .rejoin(&mut t_relay, &session_id)
            .map_err(|e| e.to_string())?;
        std::thread::sleep(Duration::from_millis(60));
        // 用**既有密钥**发一帧作为探针
        let n = ctrl
            .send_frames(&mut c_relay, &mut c_crypto, 1)
            .map_err(|e| e.to_string())?;
        if n.is_empty() {
            return Err("no frame encoded".to_string());
        }
        match target.recv_frames(&mut t_relay, &mut t_crypto, 1) {
            Ok(f) if !f.is_empty() => Ok(()),
            _ => Err("probe frame not received".to_string()),
        }
    })
    .expect("session resume should succeed");

    println!(
        "session resumed after {} retries (same keys, same session id)",
        retries
    );

    // 恢复后再连续收 2 帧，确认不是"只通一帧"
    let after = {
        let n = ctrl
            .send_frames(&mut c_relay, &mut c_crypto, 2)
            .expect("send");
        let frames = target
            .recv_frames(&mut t_relay, &mut t_crypto, 2)
            .expect("recv");
        (n.len(), frames.len())
    };
    println!("after resume: sent {} received {}", after.0, after.1);

    let _ = relay_child.kill();
    if after.1 != 2 {
        println!("FAIL: frames not delivered after resume");
        std::process::exit(1);
    }
    println!("SESSION RESUME PASSED (crypto session survived relay restart)");
}
