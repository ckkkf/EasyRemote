//! 断线重连端到端：中继被杀掉后重启，客户端按退避策略恢复。
//!
//! 验证点：
//!   1. 正常收发
//!   2. 杀掉中继后收发失效（不是假装还在）
//!   3. 中继恢复后按退避重连成功，收发恢复

use std::net::SocketAddr;
use std::process::{Child, Command, Stdio};
use std::time::Duration;

use er_session::backoff::{retry_until, BackoffPolicy};
use er_session::relay_client::RelayClient;

const SECRET: &[u8] = b"dev-secret-change-me";
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

fn exchange(a: &mut RelayClient, b: &mut RelayClient, payload: &[u8], timeout: Duration) -> bool {
    if a.send(payload).is_err() {
        return false;
    }
    match b.recv_timeout(timeout) {
        Ok(Some(msg)) => msg == payload,
        _ => false,
    }
}

fn main() {
    let port = 19131u16;
    let addr: SocketAddr = format!("127.0.0.1:{port}").parse().unwrap();

    let mut child = spawn_relay(port);
    std::thread::sleep(Duration::from_millis(700));

    let sid = "ses-reconnect-1";
    let mut a = RelayClient::new(addr, sid, "devA").unwrap();
    let mut b = RelayClient::new(addr, sid, "devB").unwrap();
    a.register_with_secret(SECRET, "devB", 600).unwrap();
    b.register_with_secret(SECRET, "devA", 600).unwrap();
    std::thread::sleep(Duration::from_millis(300));

    if !exchange(&mut a, &mut b, b"before-restart", Duration::from_secs(2)) {
        println!("FAIL: initial exchange failed");
        let _ = child.kill();
        std::process::exit(1);
    }
    println!("ok: initial exchange works");

    // 杀掉中继
    let _ = child.kill();
    let _ = child.wait();
    std::thread::sleep(Duration::from_millis(300));

    // 此时收发应失败（UDP 无连接，发送往往成功但没人转发）
    let still_works = exchange(&mut a, &mut b, b"during-outage", Duration::from_millis(800));
    if still_works {
        println!("FAIL: relay is supposed to be down, but exchange succeeded");
        std::process::exit(1);
    }
    println!("ok: exchange fails while relay is down");

    // 重启中继：客户端按退避重新注册并恢复
    let mut child = spawn_relay(port);
    std::thread::sleep(Duration::from_millis(700));

    let policy = BackoffPolicy {
        base_ms: 200,
        factor: 2,
        max_ms: 2000,
        max_attempts: 12,
    };
    let (_, retries) = retry_until(policy, Duration::from_secs(15), || {
        // 每轮重连：重新注册两端并尝试一次收发
        a.register_with_secret(SECRET, "devB", 600)
            .map_err(|e| e.to_string())?;
        b.register_with_secret(SECRET, "devA", 600)
            .map_err(|e| e.to_string())?;
        std::thread::sleep(Duration::from_millis(50));
        if exchange(&mut a, &mut b, b"after-restart", Duration::from_secs(1)) {
            Ok(())
        } else {
            Err("exchange failed".to_string())
        }
    })
    .expect("reconnect should succeed");

    println!("ok: reconnected after {} retries with backoff", retries);
    let _ = child.kill();
    println!("RECONNECT E2E PASSED");
}
