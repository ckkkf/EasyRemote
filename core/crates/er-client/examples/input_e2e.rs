//! 输入回传端到端：主控发键鼠事件 → 中继 → 被控端注入。
//!
//! 同时验证视频帧与输入事件在同一加密通道上混流而不互相干扰。

use std::net::SocketAddr;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use er_client::agent::{ControllerAgent, TargetAgent};
use er_input::{InputEvent, MouseButton, RecordingInjector};

fn main() {
    let base = std::env::args().nth(1).unwrap_or_else(|| "http://127.0.0.1:18099".into());
    let relay_addr: SocketAddr = std::env::args()
        .nth(2)
        .unwrap_or_else(|| "127.0.0.1:19121".into())
        .parse()
        .expect("relay addr");

    let target =
        TargetAgent::start(&base, "13700060001", "123456", relay_addr).expect("target start");
    let ctrl =
        ControllerAgent::start(&base, "13700060002", "123456", relay_addr).expect("ctrl start");

    let code = target.device_code();
    let captcha = target.rotate_captcha().expect("captcha");
    let session_id = ctrl.request_session(&code, &captcha).expect("session");
    target.consent(&session_id).expect("consent");

    let (mut c_relay, mut c_crypto, c_keys, c_msg) = ctrl.begin(&session_id).expect("ctrl begin");
    let (mut t_relay, mut t_crypto, t_msg) = target.begin(&session_id).expect("target begin");
    ctrl.announce(&mut c_relay, &c_msg).expect("announce c");
    target.announce(&mut t_relay, &t_msg).expect("announce t");
    target.finish(&mut t_relay, &mut t_crypto, &session_id).expect("finish t");
    ctrl.finish(&mut c_relay, &mut c_crypto, &c_keys, &session_id).expect("finish c");

    // 主控发送 3 个输入事件 + 2 帧视频
    let events = vec![
        InputEvent::Key { scancode: 0x04, pressed: true },
        InputEvent::MouseMove { x: 640, y: 360 },
        InputEvent::MouseButton { button: MouseButton::Left, pressed: true },
    ];

    let to_send = events.clone();
    let (tx, rx) = mpsc::channel::<usize>();
    let sender = thread::spawn(move || {
        ctrl.send_frames(&mut c_relay, &mut c_crypto, 2).expect("frames");
        ctrl.send_input(&mut c_relay, &mut c_crypto, &to_send).expect("input");
        tx.send(to_send.len()).expect("signal");
    });

    // 被控端先收视频，再收输入
    let frames = target.recv_frames(&mut t_relay, &mut t_crypto, 2).expect("recv frames");
    // 默认用记录型注入器（不打扰用户）；ER_REAL_INJECT=1 时走真实 CGEvent。
    let real = std::env::var("ER_REAL_INJECT").is_ok();
    #[cfg(target_os = "macos")]
    let mut mac = er_input::macos::MacInjector::new(true);
    let mut recording = RecordingInjector::live();
    let injector: &mut dyn er_input::Injector = if real {
        #[cfg(target_os = "macos")]
        {
            &mut mac
        }
        #[cfg(not(target_os = "macos"))]
        {
            &mut recording
        }
    } else {
        &mut recording
    };
    let got = target
        .recv_input(&mut t_relay, &mut t_crypto, injector, 3, Duration::from_secs(10))
        .expect("recv input");
    let _ = rx.recv();
    sender.join().expect("sender");

    println!("frames={} inputs={}", frames.len(), got.len());
    for e in &got {
        println!("  {:?}", e);
    }

    if frames.len() != 2 {
        println!("FAIL: expected 2 frames, got {}", frames.len());
        std::process::exit(1);
    }
    if got.len() != 3 {
        println!("FAIL: expected 3 input events, got {}", got.len());
        std::process::exit(1);
    }
    // 记录型注入器模式下校验计数
    if !real && recording.count() != 3 {
        println!("FAIL: injector recorded {} events", recording.count());
        std::process::exit(1);
    }
    // 内容与发送一致
    if got[0] != events[0] || got[1] != events[1] || got[2] != events[2] {
        println!("FAIL: input events corrupted in transit");
        std::process::exit(1);
    }
    println!("INPUT E2E PASSED (video + input multiplexed on the encrypted channel)");
}
