//! ABR 数据面 · 主控端（macOS，视频接收侧 + 反馈发送侧）。
//!
//! 这里才是「链路测量点」：只有真正在收视频的一方知道丢帧/丢包/延迟。
//! 每个周期（默认 1s）：
//!   1. 用视频帧头 seq 算帧级丢帧率
//!   2. 用中继重组器的分片统计算数据报级丢包率（ABR 的主口径）
//!   3. 回显最新一帧的发送时刻（被控端据此算 RTT）
//!   4. 通过可靠反向通道回传 `MSG_FEEDBACK`（3 份冗余 + 累积值）
//!
//! 用法：
//!   abr_controller <gateway> <relay_udp> <phone> <sms> <hello_file> <seconds> [cycle_ms]

use std::net::SocketAddr;
use std::time::{Duration, Instant};

use er_client::agent::{now_ms, ControllerAgent, MSG_VIDEO};
use er_client::link::{FeedbackReport, LinkStats, VideoHeader};
use er_codec::H264Decoder;

fn arg(i: usize, default: &str) -> String {
    std::env::args()
        .nth(i)
        .unwrap_or_else(|| default.to_string())
}

fn main() {
    let base = arg(1, "http://127.0.0.1:18090");
    let relay_addr: SocketAddr = arg(2, "127.0.0.1:19001").parse().expect("relay addr");
    let phone = arg(3, "13700007202");
    let sms = arg(4, "123456");
    let hello_file = arg(5, "win_hello.txt");
    let seconds: u64 = arg(6, "25").parse().expect("seconds");
    let cycle_ms: u64 = arg(7, "1000").parse().expect("cycle_ms");

    let deadline = Instant::now() + Duration::from_secs(180);
    let hello = loop {
        if let Ok(s) = std::fs::read_to_string(&hello_file) {
            if s.contains("captcha=") {
                break s;
            }
        }
        if Instant::now() > deadline {
            panic!("hello file never appeared: {hello_file}");
        }
        std::thread::sleep(Duration::from_millis(300));
    };
    let field = |key: &str| -> String {
        for line in hello.lines() {
            if let Some(rest) = line.strip_prefix(key) {
                return rest.trim().to_string();
            }
        }
        panic!("missing {key}");
    };
    let code = field("device_code=");
    let captcha = field("captcha=");

    let ctrl = ControllerAgent::start(&base, &phone, &sms, relay_addr).expect("ctrl start");
    let session_id = ctrl
        .request_session(&code, &captcha)
        .expect("request session");
    println!("ABR_RX_BOOT: session={session_id} relay={relay_addr}");
    std::thread::sleep(Duration::from_millis(1500));
    let (mut relay, mut crypto, keys, msg) = ctrl.begin(&session_id).expect("ctrl begin");
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay, &msg).expect("announce");
    ctrl.finish(&mut relay, &mut crypto, &keys, &session_id)
        .expect("handshake");
    println!("ABR_RX_BOOT: handshake_verified");

    let mut stats = LinkStats::new();
    let mut dec = H264Decoder::new().expect("decoder");
    let mut decoded_dims: Option<(usize, usize)> = None;
    let mut header_dims: Option<(u16, u16)> = None;
    let mut reports = 0;
    let start = Instant::now();
    let deadline = start + Duration::from_secs(seconds);
    let mut next_cycle = Instant::now() + Duration::from_millis(cycle_ms);
    let mut frames_total = 0u32;

    while Instant::now() < deadline {
        // 收包（短超时）→ 视频帧记账
        match relay.recv_timeout(Duration::from_millis(20)) {
            Ok(Some(sealed)) => {
                let plain = match crypto.open(&sealed) {
                    Ok(p) => p,
                    Err(_) => continue,
                };
                if plain.is_empty() || plain[0] != MSG_VIDEO {
                    continue;
                }
                let Some(hdr) = VideoHeader::decode(&plain[1..]) else {
                    continue;
                };
                stats.on_frame(&hdr, now_ms() as u32);
                frames_total += 1;

                // 分辨率证据：只在帧头尺寸变化时真解一次码（软件解码很贵，
                // 每帧都解会拖慢接收队列，反倒制造假的丢包）
                let dims_now = (hdr.width, hdr.height);
                if header_dims != Some(dims_now) {
                    header_dims = Some(dims_now);
                    let payload = &plain[1 + er_client::link::VIDEO_HEADER_LEN..];
                    match dec.decode(payload) {
                        Ok(Some((w, h, _))) => {
                            decoded_dims = Some((w, h));
                            println!(
                                "ABR_RX_DIMS: t={:.1}s header={}x{} decoded={}x{}",
                                start.elapsed().as_secs_f64(),
                                dims_now.0,
                                dims_now.1,
                                w,
                                h
                            );
                        }
                        Ok(None) => {}
                        Err(e) => println!("ABR_RX_ERR: decode: {e}"),
                    }
                }
            }
            Ok(None) => {}
            Err(e) => {
                println!("ABR_RX_ERR: relay: {e}");
                break;
            }
        }

        // 周期边界：出一份反馈
        if Instant::now() >= next_cycle {
            next_cycle += Duration::from_millis(cycle_ms);
            let frag = relay.link_stats(400);
            if let Some(report) = stats.take_cycle(now_ms() as u32, frag) {
                let copies = ctrl
                    .send_feedback(&mut relay, &mut crypto, &report, 3)
                    .unwrap_or(0);
                reports += 1;
                println!(
                    "ABR_RX_CYCLE: t={:.1}s seq={} cycle_expected={} cycle_received={} frame_loss={:.1}% cum_expected={} cum_received={} frag_recv={} frag_missing={} datagram_loss={:.1}% echo_ts={} bytes=0 sent_copies={}",
                    start.elapsed().as_secs_f64(),
                    report.seq,
                    report.cycle_expected,
                    report.cycle_received,
                    report.loss_pct,
                    report.cum_expected,
                    report.cum_received,
                    report.frag_received,
                    report.frag_missing,
                    report.datagram_loss_pct,
                    report.echo_ts_ms,
                    copies
                );
            } else {
                println!(
                    "ABR_RX_CYCLE: t={:.1}s no_frames_in_cycle（本周期一帧都没到）",
                    start.elapsed().as_secs_f64()
                );
            }
        }
    }

    let frag = relay.link_stats(400);
    let (cum_expected, cum_received) = stats.cumulative();
    println!(
        "ABR_RX_SUMMARY: frames_total={frames_total} reports={reports} cum_expected={cum_expected} cum_received={cum_received} frame_loss={:.1}% frag_recv={} frag_missing={} datagram_loss={:.2}% header_dims={:?} decoded_dims={:?} elapsed_ms={}",
        if cum_expected == 0 { 0.0 } else { (cum_expected.saturating_sub(cum_received)) as f32 * 100.0 / cum_expected as f32 },
        frag.fragments_received,
        frag.fragments_missing,
        frag.loss_pct(),
        header_dims,
        decoded_dims,
        start.elapsed().as_millis()
    );
    println!("ABR_RX_DONE session={session_id}");
    let _ = FeedbackReport::LEN;
}
