//! ER-500 transport spike: measure QUIC loopback handshake / datagram throughput / RTT.
//!
//! Run: cargo run -p er-transport --example transport_spike
//! Numbers are local loopback only; they are a lower bound, not a device/WAN benchmark.

use std::error::Error;
use std::time::Instant;

use er_common::protocol::MediaHeader;
use er_transport::quic::{QuicListener, QuicTransport};
use er_transport::{Channel, Transport};

const FRAMES: usize = 4000;
const PAYLOAD: usize = 1100;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let listener = QuicListener::bind("127.0.0.1:0".parse()?)?;
    let addr = listener.local_addr();
    let cert = listener.cert_der().to_vec();
    let accept = tokio::spawn(async move { listener.accept().await });

    let t0 = Instant::now();
    let mut client = QuicTransport::connect("127.0.0.1:0".parse()?, addr, cert).await?;
    let handshake = t0.elapsed();
    let mut server = accept.await??;

    let recv = tokio::spawn(async move {
        let mut n = 0usize;
        while let Ok(Some(_)) = tokio::time::timeout(
            std::time::Duration::from_millis(500),
            server.recv_datagram(),
        )
        .await
        {
            n += 1;
        }
        n
    });

    let payload = vec![0x5a_u8; PAYLOAD];
    let start = Instant::now();
    let mut sent = 0usize;
    for i in 0..FRAMES {
        let header = MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq: (i % u16::MAX as usize) as u16,
            flags: 0,
            ts_ms: i as u32,
        };
        match client.send_datagram(Channel::Video, header, payload.clone()) {
            Ok(()) => sent += 1,
            Err(_) => {
                tokio::task::yield_now().await;
                continue;
            }
        }
        if i % 64 == 0 {
            tokio::task::yield_now().await;
        }
    }
    let received = recv.await?;
    let elapsed = start.elapsed();
    let bytes = (received * (8 + PAYLOAD)) as f64;
    let mbps = bytes * 8.0 / elapsed.as_secs_f64() / 1_000_000.0;
    println!(
        "{{\"handshake_ms\":{:.3},\"sent\":{},\"received\":{},\"bytes\":{:.0},\"mbps\":{:.1},\"rtt_us\":{},\"elapsed_ms\":{:.1}}}",
        handshake.as_secs_f64() * 1000.0,
        sent,
        received,
        bytes,
        mbps,
        client.rtt().as_micros(),
        elapsed.as_secs_f64() * 1000.0
    );
    client.shutdown();
    Ok(())
}
