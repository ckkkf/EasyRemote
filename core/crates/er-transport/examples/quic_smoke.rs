//! 手动冒烟：逐步打印 QUIC 握手与收发，用于定位卡点。
use std::net::SocketAddr;
use std::time::Duration;

use er_common::protocol::MediaHeader;
use er_transport::quic::QuicTransport;
use er_transport::{Channel, Transport};

fn hdr(seq: u16) -> MediaHeader {
    MediaHeader { frame_type: 1, stream_id: 0, seq, flags: 0, ts_ms: 0 }
}

#[tokio::main(flavor = "multi_thread", worker_threads = 2)]
async fn main() {
    let (tx, rx) = tokio::sync::oneshot::channel::<(SocketAddr, Vec<u8>)>();

    let server = tokio::spawn(async move {
        eprintln!("[server] accepting...");
        let bind: SocketAddr = "127.0.0.1:0".parse().unwrap();
        let (local, mut t, cert) = QuicTransport::accept_one(bind).await.unwrap();
        eprintln!("[server] accepted on {local}, cert {} bytes", cert.len());
        tx.send((local, cert)).unwrap();

        let d = tokio::time::timeout(Duration::from_secs(8), t.recv_datagram()).await;
        eprintln!("[server] datagram: {:?}", d.map(|o| o.map(|f| f.payload)));

        let r = tokio::time::timeout(Duration::from_secs(8), t.recv_reliable()).await;
        eprintln!("[server] reliable: {:?}", r.map(|o| o.map(|f| f.payload)));

        t.send_reliable(Channel::Control, b"pong".to_vec()).unwrap();
        eprintln!("[server] sent pong");
        "done"
    });

    let (addr, cert) = rx.await.unwrap();
    eprintln!("[client] server at {addr}");
    let bind: SocketAddr = "127.0.0.1:0".parse().unwrap();
    let mut client = QuicTransport::connect(bind, addr, cert).await.unwrap();
    eprintln!("[client] connected, remote={}", client.remote_address());

    client.send_datagram(Channel::Video, hdr(7), vec![1, 2, 3]).unwrap();
    eprintln!("[client] datagram sent");
    client.send_reliable(Channel::Control, b"ping".to_vec()).unwrap();
    eprintln!("[client] reliable queued");

    let pong = tokio::time::timeout(Duration::from_secs(8), client.recv_reliable()).await;
    eprintln!("[client] pong: {:?}", pong.map(|o| o.map(|f| f.payload)));

    let s = server.await.unwrap();
    eprintln!("[server] result: {s}");
}
