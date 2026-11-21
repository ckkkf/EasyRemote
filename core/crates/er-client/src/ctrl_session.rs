//! 主控会话（给客户端渲染用）：连上被控端后，后台线程持续收帧→解码→存最新一帧，
//! 同时把 UI 发来的输入事件转发出去。
//!
//! 为什么单独一个模块：`pair.rs` 是本机双端自检，`agent.rs` 是"跑完就结束"的脚本式流程；
//! 客户端要的是**长驻会话 + 随时取最新帧**，所以这里把循环、双缓冲、输入通道都收进来。

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use er_input::InputEvent;

use crate::agent::{ControllerAgent, MSG_VIDEO};
use crate::link::VideoHeader;

/// 最新解码帧（RGBA，给 Flutter 的 decodeImageFromPixels 用）。
#[derive(Clone)]
pub struct Frame {
    pub width: usize,
    pub height: usize,
    pub rgba: Vec<u8>,
}

/// 会话运行状态。
#[derive(Default, Clone)]
pub struct CtrlStats {
    pub connected: bool,
    pub session_id: String,
    pub frames: u64,
    pub last_error: String,
}

pub struct CtrlSession {
    pub id: String,
    pub latest: Arc<Mutex<Option<Frame>>>,
    pub stats: Arc<Mutex<CtrlStats>>,
    input_tx: Sender<InputEvent>,
    stop: Arc<AtomicBool>,
}

impl CtrlSession {
    pub fn send_input(&self, ev: InputEvent) {
        let _ = self.input_tx.send(ev);
    }

    pub fn snapshot(&self) -> Option<Frame> {
        self.latest.lock().ok().and_then(|g| g.clone())
    }

    pub fn stop(&self) {
        self.stop.store(true, Ordering::Relaxed);
    }
}
/// 发起主控会话：登录 → 用设备码 + 临时密码建会话 → 后台收帧/发输入。
///
/// 返回后立刻可用：`snapshot()` 拿最新帧，`send_input()` 发事件。
pub fn connect(
    gateway: &str,
    relay: std::net::SocketAddr,
    phone: &str,
    sms_code: &str,
    device_code: &str,
    password: &str,
) -> Result<CtrlSession, String> {
    let ctrl =
        ControllerAgent::start(gateway, phone, sms_code, relay).map_err(|e| e.to_string())?;
    let session_id = ctrl
        .request_session(device_code, password)
        .map_err(|e| e.to_string())?;

    // 等被控端 consent 落地（真实产品里是对方点「接受」）
    std::thread::sleep(Duration::from_millis(1500));
    let (mut relay_cli, mut crypto, keys, msg) =
        ctrl.begin(&session_id).map_err(|e| e.to_string())?;
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay_cli, &msg)
        .map_err(|e| e.to_string())?;
    ctrl.finish(&mut relay_cli, &mut crypto, &keys, &session_id)
        .map_err(|e| e.to_string())?;

    let latest: Arc<Mutex<Option<Frame>>> = Arc::new(Mutex::new(None));
    let stats: Arc<Mutex<CtrlStats>> = Arc::new(Mutex::new(CtrlStats {
        connected: true,
        session_id: session_id.clone(),
        frames: 0,
        last_error: String::new(),
    }));
    let stop = Arc::new(AtomicBool::new(false));
    let (input_tx, input_rx) = channel::<InputEvent>();

    let id = format!("ctrl-{}", nanos_hex());
    let id_for_thread = id.clone();
    let latest_thread = latest.clone();
    let stats_thread = stats.clone();
    let stop_thread = stop.clone();

    std::thread::spawn(move || {
        run_loop(
            &ctrl,
            &mut relay_cli,
            &mut crypto,
            &session_id,
            &id_for_thread,
            latest_thread,
            stats_thread,
            stop_thread,
            input_rx,
        );
    });

    Ok(CtrlSession {
        id,
        latest,
        stats,
        input_tx,
        stop,
    })
}

fn nanos_hex() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| format!("{:x}", d.as_nanos()))
        .unwrap_or_else(|_| "0".to_string())
}
#[allow(clippy::too_many_arguments)]
fn run_loop(
    ctrl: &ControllerAgent,
    relay: &mut er_session::relay_client::RelayClient,
    crypto: &mut er_session::crypto::CryptoSession,
    session_id: &str,
    _id: &str,
    latest: Arc<Mutex<Option<Frame>>>,
    stats: Arc<Mutex<CtrlStats>>,
    stop: Arc<AtomicBool>,
    input_rx: Receiver<InputEvent>,
) {
    let _ = session_id;
    let mut dec = match er_codec::H264Decoder::new() {
        Ok(d) => d,
        Err(e) => {
            if let Ok(mut s) = stats.lock() {
                s.last_error = format!("decoder: {e}");
                s.connected = false;
            }
            return;
        }
    };
    let mut last_decode = Instant::now() - Duration::from_secs(1);
    // 心跳：让被控端能区分「主控只是没动鼠标」和「主控已经走了」
    let mut last_beat = Instant::now() - Duration::from_secs(10);

    while !stop.load(Ordering::Relaxed) {
        if last_beat.elapsed() >= Duration::from_secs(2) {
            last_beat = Instant::now();
            let beat = [crate::agent::MSG_HEARTBEAT];
            let sealed = crypto.seal(&beat);
            let _ = relay.send(&sealed);
        }
        // ① 先把输入发出去（UI 可能一次给很多个移动）
        while let Ok(ev) = input_rx.try_recv() {
            let _ = ctrl.send_input(relay, crypto, &[ev]);
        }

        // ② 收一帧（短超时，保证循环能响应 stop）
        match relay.recv_timeout(Duration::from_millis(10)) {
            Ok(Some(sealed)) => {
                let Ok(plain) = crypto.open(&sealed) else {
                    continue;
                };
                if plain.is_empty() || plain[0] != MSG_VIDEO {
                    continue;
                }
                if plain.len() < 1 + crate::link::VIDEO_HEADER_LEN {
                    continue;
                }
                let Some(_hdr) = VideoHeader::decode(&plain[1..]) else {
                    continue;
                };
                let payload = &plain[1 + crate::link::VIDEO_HEADER_LEN..];
                // 软解 1080p ~90ms：限到 ~12fps，否则解码队列越拖越长、UI 也追不上
                if last_decode.elapsed() < Duration::from_millis(80) {
                    continue;
                }
                if let Ok(Some((w, h, rgb))) = dec.decode(payload) {
                    last_decode = Instant::now();
                    // 转 RGBA：Flutter 的 decodeImageFromPixels 要 rgba8888
                    let mut rgba = Vec::with_capacity(w * h * 4);
                    for px in rgb.chunks_exact(3) {
                        rgba.push(px[0]);
                        rgba.push(px[1]);
                        rgba.push(px[2]);
                        rgba.push(255);
                    }
                    if let Ok(mut g) = latest.lock() {
                        *g = Some(Frame {
                            width: w,
                            height: h,
                            rgba,
                        });
                    }
                    if let Ok(mut s) = stats.lock() {
                        s.frames += 1;
                    }
                }
            }
            _ => {}
        }
    }

    let _ = ctrl.rejoin(relay, session_id);
    if let Ok(mut s) = stats.lock() {
        s.connected = false;
    }
}
/// 接管一个已建立的会话（客户端已在 UI 层 request 过，这里只跑收发）。
///
/// 与 `connect` 的区别：不重新登录、不重新 request，避免「设备正在被控制」。
pub fn attach(
    gateway: &str,
    relay: std::net::SocketAddr,
    token: &str,
    device_id: &str,
    device_token: &str,
    session_id: &str,
) -> Result<CtrlSession, String> {
    let ctrl = ControllerAgent::from_session(gateway, token, device_id, device_token, relay)
        .map_err(|e| e.to_string())?;
    let (mut relay_cli, mut crypto, keys, msg) =
        ctrl.begin(session_id).map_err(|e| e.to_string())?;
    // 时序对齐：被控端收到 consent 后要 sleep 1.2s → begin → 再 sleep 2.5s 才 announce，
    // 也就是说它在中继上「可收包」大约在 3.7s 之后。主控 600ms 就 announce 的话，
    // 中继会因对端未注册直接丢包，接着 finish 干等 15s —— 就是用户看到的
    // 「no peer handshake within 15s」。这里对齐到 3s，并且握手失败时重发（幂等）。
    std::thread::sleep(Duration::from_millis(3000));
    ctrl.announce(&mut relay_cli, &msg)
        .map_err(|e| e.to_string())?;
    let mut handshake_ok = false;
    for _ in 0..3 {
        if ctrl
            .finish(&mut relay_cli, &mut crypto, &keys, session_id)
            .is_ok()
        {
            handshake_ok = true;
            break;
        }
        let _ = ctrl.rejoin(&mut relay_cli, session_id);
        let _ = ctrl.announce(&mut relay_cli, &msg);
    }
    if !handshake_ok {
        return Err("handshake failed after 3 attempts".to_string());
    }

    let latest: Arc<Mutex<Option<Frame>>> = Arc::new(Mutex::new(None));
    let stats: Arc<Mutex<CtrlStats>> = Arc::new(Mutex::new(CtrlStats {
        connected: true,
        session_id: session_id.to_string(),
        frames: 0,
        last_error: String::new(),
    }));
    let stop = Arc::new(AtomicBool::new(false));
    let (input_tx, input_rx) = channel::<InputEvent>();
    let id = format!("ctrl-{}", nanos_hex());

    let id_for_thread = id.clone();
    let latest_thread = latest.clone();
    let stats_thread = stats.clone();
    let stop_thread = stop.clone();
    let sid = session_id.to_string();

    std::thread::spawn(move || {
        run_loop(
            &ctrl,
            &mut relay_cli,
            &mut crypto,
            &sid,
            &id_for_thread,
            latest_thread,
            stats_thread,
            stop_thread,
            input_rx,
        );
    });

    Ok(CtrlSession {
        id,
        latest,
        stats,
        input_tx,
        stop,
    })
}
