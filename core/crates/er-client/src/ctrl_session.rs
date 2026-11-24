//! 主控会话（给客户端渲染用）：连上被控端后，后台线程持续收帧→解码→存最新一帧，
//! 同时把 UI 发来的输入事件转发出去。
//!
//! 为什么单独一个模块：`pair.rs` 是本机双端自检，`agent.rs` 是"跑完就结束"的脚本式流程；
//! 客户端要的是**长驻会话 + 随时取最新帧**，所以这里把循环、双缓冲、输入通道都收进来。

use std::collections::VecDeque;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use er_input::InputEvent;
use er_session::relay_client::{p2p_token, P2PConfig};

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
    /// 最近一秒实际解码帧率（present 侧由 Flutter 轮询计数对照）。
    pub decode_fps: u32,
    /// 最近一帧解码耗时（毫秒）。
    pub decode_ms: u64,
    pub dropped_frames: u64,
    /// 最近一秒收到的媒体码率（kbps）。
    pub bitrate_kbps: u32,
    /// 分片级丢包率（%），来自数据面重组统计。
    pub loss_pct: f32,
    pub input_acks: u64,
    /// 输入 ACK P95 延迟（ms）。
    pub input_p95_ms: u32,
    pub last_error: String,
}

/// 输入 ACK 延迟 P95（空样本返回 0）。
pub fn input_p95_ms(samples: &[u32]) -> u32 {
    if samples.is_empty() {
        return 0;
    }
    let mut sorted = samples.to_vec();
    sorted.sort_unstable();
    let idx = ((sorted.len() as f64 * 0.95) as usize).min(sorted.len() - 1);
    sorted[idx]
}

/// 关键帧请求策略：丢包/解码失败时请求；最多 2 次/秒。
pub fn should_request_keyframe(
    last_seq: Option<u32>,
    seq: u32,
    since_last_request: Duration,
) -> bool {
    let gap = match last_seq {
        Some(prev) => seq.wrapping_sub(prev) > 1,
        None => false,
    };
    gap && since_last_request >= Duration::from_millis(500)
}

/// 统计真实解码 FPS 的窗口计数器（1 秒窗口，测试可注入时间）。
pub struct FpsCounter {
    window_start: Instant,
    frames: u32,
    last_fps: u32,
}

impl FpsCounter {
    pub fn new(now: Instant) -> Self {
        Self {
            window_start: now,
            frames: 0,
            last_fps: 0,
        }
    }

    pub fn tick(&mut self, now: Instant) {
        self.frames += 1;
        let elapsed = now.saturating_duration_since(self.window_start);
        if elapsed >= Duration::from_secs(1) {
            self.last_fps = (self.frames as f64 / elapsed.as_secs_f64()).round() as u32;
            self.frames = 0;
            self.window_start = now;
        }
    }

    pub fn fps(&self) -> u32 {
        self.last_fps
    }
}

/// attach 失败的结构化信息（ER-905 4.1(4)）。
///
/// 为什么要结构化：FFI 只回一个拼接字符串时，UI 无法区分
/// code/name/message/trace_id，只能把整串（含内部 URL/body）贴到画面上。
#[derive(Debug, Clone)]
pub struct AttachError {
    pub code: String,
    pub name: String,
    pub message: String,
    pub retryable: bool,
    pub trace_id: String,
}

impl From<crate::agent::AgentError> for AttachError {
    /// AgentError 现在只是 String 包装，但底层失败往往来自 ServiceError；
    /// 这里保守地用稳定分类 + 原文，避免把内部 URL 泄进 message。
    fn from(e: crate::agent::AgentError) -> Self {
        // 只处理**传输层/本地**错误：真正的网关错误走 ServiceError → AttachError，
        // 保留 trace_id；这里绝不靠字符串猜 HTTP 状态。
        let msg = e.to_string();
        let (code, retryable) = if msg.contains("no peer handshake") {
            ("HANDSHAKE", true)
        } else {
            ("AGENT", false)
        };
        Self {
            code: code.into(),
            name: String::new(),
            // 只保留可读部分：剥离 URL 与 body=
            message: msg.split(" body=").next().unwrap_or(&msg).to_string(),
            retryable,
            trace_id: String::new(),
        }
    }
}

impl From<crate::ServiceError> for AttachError {
    fn from(e: crate::ServiceError) -> Self {
        Self {
            code: e.code,
            name: e.name,
            message: e.message,
            retryable: e.retryable,
            trace_id: e.trace_id,
        }
    }
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
) -> Result<CtrlSession, AttachError> {
    let ctrl =
        ControllerAgent::start(gateway, phone, sms_code, relay).map_err(|e| AttachError {
            code: "AGENT".into(),
            name: String::new(),
            message: e.to_string(),
            retryable: false,
            trace_id: String::new(),
        })?;
    let session_id = ctrl
        .request_session(device_code, password)
        .map_err(AttachError::from)?;

    // 等被控端 consent 落地（真实产品里是对方点「接受」）
    std::thread::sleep(Duration::from_millis(1500));
    let (mut relay_cli, mut crypto, keys, msg) =
        ctrl.begin_ex(&session_id).map_err(AttachError::from)?;
    let _ = relay_cli.negotiate_p2p(&P2PConfig::new(
        &session_id,
        p2p_token(&session_id),
        Duration::from_secs(2),
    ));
    std::thread::sleep(Duration::from_millis(2500));
    ctrl.announce(&mut relay_cli, &msg).map_err(|e| {
        // 本地中继错误原文可能含地址/路径：只进日志，UI 用稳定文案。
        eprintln!("attach announce error: {e}");
        AttachError {
            code: "RELAY".into(),
            name: String::new(),
            message: "中继连接失败，请稍后重试".into(),
            retryable: true,
            trace_id: String::new(),
        }
    })?;
    ctrl.finish_ex(&mut relay_cli, &mut crypto, &keys, &session_id, None)
        .map_err(AttachError::from)?;
    ctrl.mark_ready_ex(&session_id).map_err(AttachError::from)?;

    let latest: Arc<Mutex<Option<Frame>>> = Arc::new(Mutex::new(None));
    let stats: Arc<Mutex<CtrlStats>> = Arc::new(Mutex::new(CtrlStats {
        connected: true,
        session_id: session_id.clone(),
        frames: 0,
        decode_fps: 0,
        decode_ms: 0,
        dropped_frames: 0,
        bitrate_kbps: 0,
        loss_pct: 0.0,
        input_acks: 0,
        input_p95_ms: 0,
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
    let mut fps_counter = FpsCounter::new(Instant::now());
    let mut last_video_seq: Option<u32> = None;
    let mut last_kf_request = Instant::now() - Duration::from_secs(1);
    let mut media_bytes = 0u64;
    let mut media_window = Instant::now();
    let mut pending_inputs: VecDeque<(u32, Instant)> = VecDeque::new();
    let mut clipboard = if std::env::var("ER_CLIPBOARD_SYNC").ok().as_deref() == Some("1") {
        Some(er_channels::clipboard::ClipboardService::new(
            er_channels::clipboard::CommandClipboard,
            true,
        ))
    } else {
        None
    };
    let mut last_clip_poll = Instant::now();
    let mut file_tx: Option<er_channels::file_stream::StreamSender> = None;
    let mut file_meta_sent = false;
    let mut file_last_send = Instant::now();
    if let Ok(path) = std::env::var("ER_FILE_SEND_PATH") {
        if let Ok(data) = std::fs::read(&path) {
            let name = std::path::Path::new(&path)
                .file_name()
                .and_then(|s| s.to_str())
                .unwrap_or("file.bin");
            let id = (std::process::id() << 16) ^ (data.len() as u32);
            if let Ok(tx) = er_channels::file_stream::StreamSender::new(
                id,
                name,
                data,
                er_channels::file_transfer::DEFAULT_CHUNK,
            ) {
                file_tx = Some(tx);
            }
        }
    }
    let mut input_samples: VecDeque<u32> = VecDeque::new();
    // 心跳：让被控端能区分「主控只是没动鼠标」和「主控已经走了」
    let mut last_beat = Instant::now() - Duration::from_secs(10);
    let mut last_state_check = Instant::now();

    while !stop.load(Ordering::Relaxed) {
        // 关闭传播兜底：被控端/后台关闭后 1s 内停止发送输入并退出。
        if last_state_check.elapsed() >= Duration::from_secs(1) {
            last_state_check = Instant::now();
            if let Ok(state) = ctrl.session_state(session_id) {
                if state == "closed" || state == "failed" || state == "ended" {
                    if let Ok(mut s) = stats.lock() {
                        s.connected = false;
                        s.last_error = format!("session {state}");
                    }
                    break;
                }
            }
        }
        if let Some(tx) = file_tx.as_mut() {
            if !file_meta_sent {
                file_meta_sent = true;
                let sealed = crypto.seal(&tx.meta());
                let _ = relay.send(&sealed);
            }
            if let Some(chunk) = tx.next_chunk() {
                file_last_send = Instant::now();
                let sealed = crypto.seal(&chunk);
                let _ = relay.send(&sealed);
            } else if file_last_send.elapsed() >= Duration::from_millis(300) {
                if let Some(chunk) = tx.retransmit() {
                    file_last_send = Instant::now();
                    let sealed = crypto.seal(&chunk);
                    let _ = relay.send(&sealed);
                }
            }
        }
        if let Some(svc) = clipboard.as_mut() {
            if last_clip_poll.elapsed() >= Duration::from_secs(1) {
                last_clip_poll = Instant::now();
                if let Ok(Some(update)) = svc.poll_local() {
                    if let Ok(msg) = er_channels::clipboard::encode_clip(&update) {
                        let sealed = crypto.seal(&msg);
                        let _ = relay.send(&sealed);
                    }
                }
            }
        }
        if media_window.elapsed() >= Duration::from_secs(1) {
            let stats_window = relay.link_stats(1000);
            let kbps =
                (media_bytes as f64 * 8.0 / media_window.elapsed().as_secs_f64() / 1000.0) as u32;
            if let Ok(mut s) = stats.lock() {
                s.bitrate_kbps = kbps;
                s.loss_pct = stats_window.loss_pct();
            }
            media_bytes = 0;
            media_window = Instant::now();
        }
        if last_beat.elapsed() >= Duration::from_secs(2) {
            last_beat = Instant::now();
            let beat = [crate::agent::MSG_HEARTBEAT];
            let sealed = crypto.seal(&beat);
            let _ = relay.send(&sealed);
        }
        // ① 先把输入发出去（UI 可能一次给很多个移动）
        while let Ok(ev) = input_rx.try_recv() {
            if let Ok(seq) = ctrl.send_input(relay, crypto, &[ev]) {
                pending_inputs.push_back((seq, Instant::now()));
                while pending_inputs.len() > 256 {
                    pending_inputs.pop_front();
                }
            }
        }

        // ② 收一帧（短超时，保证循环能响应 stop）
        if let Ok(Some(sealed)) = relay.recv_timeout(Duration::from_millis(10)) {
            let Ok(plain) = crypto.open(&sealed) else {
                continue;
            };
            if !plain.is_empty() && plain[0] == er_channels::file_stream::MSG_FILE_ACK {
                if let (Some(tx), true) = (file_tx.as_mut(), plain.len() >= 9) {
                    let id = u32::from_be_bytes([plain[1], plain[2], plain[3], plain[4]]);
                    let index = u32::from_be_bytes([plain[5], plain[6], plain[7], plain[8]]);
                    if tx.id() == id {
                        let _ = tx.ack(index);
                    }
                }
                continue;
            }
            if !plain.is_empty() && plain[0] == er_channels::clipboard::MSG_CLIP {
                if let Some(svc) = clipboard.as_mut() {
                    if let Some(update) = er_channels::clipboard::decode_clip(&plain) {
                        let _ = svc.apply_remote(&update);
                    }
                }
                continue;
            }
            if !plain.is_empty() && plain[0] == crate::agent::MSG_INPUT_ACK {
                if plain.len() >= 5 {
                    let seq = u32::from_be_bytes([plain[1], plain[2], plain[3], plain[4]]);
                    if let Some(pos) = pending_inputs.iter().position(|(s, _)| *s == seq) {
                        let (_, sent_at) = pending_inputs.remove(pos).unwrap();
                        let rtt = sent_at.elapsed().as_millis() as u32;
                        input_samples.push_back(rtt);
                        while input_samples.len() > 100 {
                            input_samples.pop_front();
                        }
                        let mut sorted: Vec<u32> = input_samples.iter().copied().collect();
                        sorted.sort_unstable();
                        let p95 = sorted
                            .get(
                                ((sorted.len() as f64 * 0.95) as usize)
                                    .min(sorted.len().saturating_sub(1)),
                            )
                            .copied()
                            .unwrap_or(0);
                        if let Ok(mut s) = stats.lock() {
                            s.input_acks += 1;
                            s.input_p95_ms = p95;
                        }
                    }
                }
                continue;
            }
            if plain.is_empty() || plain[0] != MSG_VIDEO {
                continue;
            }
            if plain.len() < 1 + crate::link::VIDEO_HEADER_LEN {
                continue;
            }
            let Some(hdr) = VideoHeader::decode(&plain[1..]) else {
                continue;
            };
            if should_request_keyframe(last_video_seq, hdr.seq, last_kf_request.elapsed()) {
                last_kf_request = Instant::now();
                let req = crypto.seal(&[crate::agent::MSG_KEYFRAME]);
                let _ = relay.send(&req);
            }
            last_video_seq = Some(hdr.seq);
            media_bytes += plain.len() as u64;
            let payload = &plain[1 + crate::link::VIDEO_HEADER_LEN..];
            // 不人为限帧：解码耗时由真实硬解/软解能力决定，FPS 通过统计上报。
            let decode_started = Instant::now();
            if let Ok(Some((w, h, rgb))) = dec.decode(payload) {
                // 转 RGBA：Flutter 的 decodeImageFromPixels 要 rgba8888
                let mut rgba = Vec::with_capacity(w * h * 4);
                let (pixels, _) = rgb.as_chunks::<3>();
                for px in pixels {
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
                fps_counter.tick(Instant::now());
                if let Ok(mut s) = stats.lock() {
                    s.frames += 1;
                    s.decode_ms = decode_started.elapsed().as_millis() as u64;
                    s.decode_fps = fps_counter.fps();
                }
            } else {
                if let Ok(mut s) = stats.lock() {
                    s.dropped_frames += 1;
                }
                // 解码失败（缺参考帧/花屏）：请求新的关键帧，最多 2 次/秒。
                if last_kf_request.elapsed() >= Duration::from_millis(500) {
                    last_kf_request = Instant::now();
                    let req = crypto.seal(&[crate::agent::MSG_KEYFRAME]);
                    let _ = relay.send(&req);
                }
            }
        }
    }

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
) -> Result<CtrlSession, AttachError> {
    let ctrl = ControllerAgent::from_session(gateway, token, device_id, device_token, relay)
        .map_err(|e| AttachError {
            code: "AGENT".into(),
            name: String::new(),
            message: e.to_string(),
            retryable: false,
            trace_id: String::new(),
        })?;
    let (mut relay_cli, mut crypto, keys, msg) =
        ctrl.begin_ex(session_id).map_err(AttachError::from)?;
    let _ = relay_cli.negotiate_p2p(&P2PConfig::new(
        session_id,
        p2p_token(session_id),
        Duration::from_secs(2),
    ));
    // 两端在握手 deadline 内重发注册和签名消息，不靠启动时间对齐。
    ctrl.finish_ex(&mut relay_cli, &mut crypto, &keys, session_id, Some(&msg))
        .map_err(AttachError::from)?;
    ctrl.mark_ready_ex(session_id).map_err(AttachError::from)?;

    let latest: Arc<Mutex<Option<Frame>>> = Arc::new(Mutex::new(None));
    let stats: Arc<Mutex<CtrlStats>> = Arc::new(Mutex::new(CtrlStats {
        connected: true,
        session_id: session_id.to_string(),
        frames: 0,
        decode_fps: 0,
        decode_ms: 0,
        dropped_frames: 0,
        bitrate_kbps: 0,
        loss_pct: 0.0,
        input_acks: 0,
        input_p95_ms: 0,
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
#[cfg(test)]
mod stats_tests {
    use super::*;

    #[test]
    fn fps_counter_reports_window_rate() {
        let t0 = Instant::now();
        let mut c = FpsCounter::new(t0);
        // 30 帧均匀分布在 1 秒内。
        for i in 1..=30 {
            c.tick(t0 + Duration::from_millis(i * 33));
        }
        c.tick(t0 + Duration::from_millis(1001));
        assert!(c.fps() >= 27 && c.fps() <= 33, "fps={}", c.fps());
    }
}
#[cfg(test)]
mod keyframe_policy_tests {
    use super::*;

    #[test]
    fn requests_on_gap_but_throttles() {
        assert!(!should_request_keyframe(None, 5, Duration::from_secs(1)));
        assert!(should_request_keyframe(Some(5), 7, Duration::from_secs(1)));
        assert!(!should_request_keyframe(Some(5), 6, Duration::from_secs(1)));
        assert!(!should_request_keyframe(
            Some(5),
            9,
            Duration::from_millis(100)
        ));
    }
}
#[cfg(test)]
mod input_p95_tests {
    use super::*;

    #[test]
    fn p95_is_window_index() {
        assert_eq!(input_p95_ms(&[]), 0);
        let s: Vec<u32> = (1..=100).collect();
        assert_eq!(input_p95_ms(&s), 96);
    }
}
