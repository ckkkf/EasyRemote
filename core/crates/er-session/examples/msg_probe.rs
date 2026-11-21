//! 诊断：经真实中继发 N 条消息，看接收端能收到多少条。
use std::net::SocketAddr;
use std::time::Duration;

use er_session::relay_client::RelayClient;

const SECRET: &[u8] = b"dev-secret-change-me";

fn main() {
    let relay: SocketAddr = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "127.0.0.1:19121".into())
        .parse()
        .unwrap();
    let sid = "ses-probe-1";
    let mut a = RelayClient::new(relay, sid, "devA").unwrap();
    let mut b = RelayClient::new(relay, sid, "devB").unwrap();
    a.register_with_secret(SECRET, "devB", 600).unwrap();
    b.register_with_secret(SECRET, "devA", 600).unwrap();
    std::thread::sleep(Duration::from_millis(300));

    for i in 0..5u8 {
        let payload = vec![i; 1000];
        a.send(&payload).unwrap();
        std::thread::sleep(Duration::from_millis(16));
    }
    let mut small = 0;
    while let Ok(Some(_)) = b.recv_timeout(Duration::from_millis(800)) {
        small += 1;
        if small >= 5 {
            break;
        }
    }
    println!("small messages received: {small}/5");

    for i in 0..3u8 {
        let payload = vec![i; 80_000];
        a.send(&payload).unwrap();
        std::thread::sleep(Duration::from_millis(50));
    }
    let mut big = 0;
    while let Ok(Some(m)) = b.recv_timeout(Duration::from_millis(1500)) {
        println!("big message {big}: {} bytes", m.len());
        big += 1;
        if big >= 3 {
            break;
        }
    }
    println!("big messages received: {big}/3");
}
