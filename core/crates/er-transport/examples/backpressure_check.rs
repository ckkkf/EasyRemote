//! B1 回归：可靠队列有界，满了必须返回 Backpressure 而不是无限增长。
use std::time::Duration;

use er_common::protocol::MediaHeader;
use er_transport::quic::{QuicListener, QuicTransport};
use er_transport::{Channel, Transport, TransportError};

#[tokio::main(flavor = "multi_thread", worker_threads = 2)]
async fn main() {
    let listener = QuicListener::bind("127.0.0.1:0".parse().unwrap()).unwrap();
    let addr = listener.local_addr();
    let cert = listener.cert_der().to_vec();

    let server = tokio::spawn(async move {
        // 服务端接受连接但**故意不读**可靠流，制造背压
        let t = listener.accept().await.unwrap();
        tokio::time::sleep(Duration::from_secs(3)).await;
        drop(t);
    });

    let mut client = QuicTransport::connect("127.0.0.1:0".parse().unwrap(), addr, cert)
        .await
        .unwrap();

    // 狂灌可靠帧，直到出现 Backpressure
    let mut hit_backpressure = false;
    for i in 0..10_000u32 {
        let payload = vec![0u8; 512];
        match client.send_reliable(Channel::Control, payload) {
            Ok(()) => {}
            Err(TransportError::Backpressure) => {
                hit_backpressure = true;
                eprintln!("backpressure after {i} frames (queue cap honoured)");
                break;
            }
            Err(e) => {
                eprintln!("unexpected error: {e}");
                std::process::exit(1);
            }
        }
        let _ = i;
    }

    // datagram 仍应可用
    let hdr = MediaHeader {
        frame_type: 1,
        stream_id: 0,
        seq: 1,
        flags: 0,
        ts_ms: 0,
    };
    let dg = client.send_datagram(Channel::Video, hdr, vec![0u8; 100]);
    let dg_ok = matches!(dg, Ok(()) | Err(TransportError::TooLarge));

    client.shutdown();
    let _ = server.await;

    if !hit_backpressure {
        eprintln!("FAIL: reliable queue never returned Backpressure");
        std::process::exit(1);
    }
    if !dg_ok {
        eprintln!("FAIL: datagram path broken");
        std::process::exit(1);
    }
    println!("B1 BACKPRESSURE CHECK PASSED");
}
