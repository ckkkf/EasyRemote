//! 流式被控：给「同一个客户端既当主控又当被控」用。
//!
//! 与 serve_host 的区别：serve_host 是「收 N 帧就返回」的一次性验证；这里是产品形态——
//! 挂在那儿等人连，连上后持续推流 + 实时注入鼠标键盘。
//!
//! **会话循环**：一个主控离开后必须回到等待状态，而不是整个线程收工。
//! 之前是单次结构，第一个连接走后所有后续请求都石沉大海（主控卡在「等待对方授权」，
//! 被控端这边一片安静）——排查时很容易被误判成「码错了」。

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, TrySendError};
use std::sync::Arc;
use std::time::Duration;

use er_session::relay_client::{p2p_token, P2PConfig};

use crate::host_consent::{ConsentBroker, ConsentSnapshot};

/// 被控流的运行状态（供 UI 轮询）。
#[derive(Default, Clone)]
pub struct HostStreamStats {
    pub running: bool,
    pub session_id: String,
    pub frames: u64,
    pub injected: u64,
    pub link: String,
    pub last_error: String,
}

struct HostStreamHandle {
    stop: Arc<AtomicBool>,
    stats: Arc<std::sync::Mutex<HostStreamStats>>,
    consent: Arc<ConsentBroker>,
}

fn streams() -> &'static std::sync::Mutex<std::collections::HashMap<String, HostStreamHandle>> {
    use std::sync::OnceLock;
    static S: OnceLock<std::sync::Mutex<std::collections::HashMap<String, HostStreamHandle>>> =
        OnceLock::new();
    S.get_or_init(|| std::sync::Mutex::new(std::collections::HashMap::new()))
}

/// 平台输入注入器（被控流用）。
pub fn input_injector() -> Box<dyn er_input::Injector> {
    #[cfg(target_os = "macos")]
    {
        Box::new(er_input::macos::MacInjector::new(true))
    }
    #[cfg(target_os = "windows")]
    {
        Box::new(er_input::windows::WindowsInjector::new(true))
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        Box::new(er_input::RecordingInjector::new())
    }
}
/// 启动后台被控流：循环「等待主控 → 授权 → 握手 → 推流」，直到 stop 被置位。
///
/// 密码由调用方负责（`TargetAgent::set_password`），这里只管会话与画面/输入。
pub fn start_host_stream(
    host_id: &str,
    signal_base: &str,
    fps: u32,
    idle_timeout_secs: u64,
    keep_awake: bool,
) -> Result<(), String> {
    let (target, _relay_addr) = crate::conductor::host_target(host_id)?;

    if streams()
        .lock()
        .map_err(|_| "stream registry poisoned".to_string())?
        .contains_key(host_id)
    {
        return Err("stream already running".to_string());
    }

    let stop = Arc::new(AtomicBool::new(false));
    let stats = Arc::new(std::sync::Mutex::new(HostStreamStats::default()));
    let consent = Arc::new(ConsentBroker::new());
    if let Ok(mut s) = stats.lock() {
        s.running = true;
    }
    streams()
        .lock()
        .map_err(|_| "stream registry poisoned".to_string())?
        .insert(
            host_id.to_string(),
            HostStreamHandle {
                stop: stop.clone(),
                stats: stats.clone(),
                consent: consent.clone(),
            },
        );

    let signal = signal_base.to_string();
    let stats_thread = stats.clone();
    let stop_thread = stop.clone();
    let consent_thread = consent.clone();
    std::thread::spawn(move || {
        const MAX_PENDING_NOTIFICATIONS: usize = 4;
        let (requests, incoming) = sync_channel(MAX_PENDING_NOTIFICATIONS);
        let signal_target = target.clone();
        let signal_stop = stop_thread.clone();
        let worker = std::thread::spawn(move || {
            let worker = || -> Result<(), String> {
                let mut conn = signal_target
                    .connect_signalling(&signal)
                    .map_err(|e| e.to_string())?;
                while let Some(sid) = conn
                    .wait_while(|| !signal_stop.load(Ordering::Relaxed))
                    .map_err(|e| e.to_string())?
                {
                    match requests.try_send(sid) {
                        Ok(()) => {}
                        Err(TrySendError::Disconnected(_)) => break,
                        Err(TrySendError::Full(sid)) => {
                            eprintln!("[host] consent queue full session_id={sid}");
                        }
                    }
                }
                Ok(())
            };
            if let Err(e) = worker() {
                eprintln!("[host] signalling receive failed: {e}");
            }
        });
        if let Err(e) = run_host_stream(
            &target,
            &incoming,
            fps,
            idle_timeout_secs,
            keep_awake,
            &stop_thread,
            &stats_thread,
            &consent_thread,
        ) {
            if let Ok(mut s) = stats_thread.lock() {
                s.last_error = e;
            }
        }
        stop_thread.store(true, Ordering::Relaxed);
        if worker.join().is_err() {
            eprintln!("[host] signalling worker panicked");
        }
        if let Ok(mut s) = stats_thread.lock() {
            s.running = false;
            s.session_id.clear();
        }
    });

    Ok(())
}
#[allow(clippy::too_many_arguments)]
fn run_host_stream(
    target: &crate::agent::TargetAgent,
    requests: &Receiver<String>,
    fps: u32,
    idle_timeout_secs: u64,
    keep_awake: bool,
    stop: &Arc<AtomicBool>,
    stats: &Arc<std::sync::Mutex<HostStreamStats>>,
    consent: &Arc<ConsentBroker>,
) -> Result<(), String> {
    let idle = std::time::Duration::from_secs(idle_timeout_secs.max(60));

    // D2 防睡眠：只在设置开启时持有 OS 抑制；guard 在函数退出/panic 时释放。
    let mut power = er_features::d2_power::PlatformInhibitor::new();
    let _power_guard = if keep_awake {
        match er_features::d2_power::SleepGuard::new(&mut power, true) {
            Ok(g) => Some(g),
            Err(e) => {
                if let Ok(mut st) = stats.lock() {
                    st.last_error = format!("prevent_sleep failed: {e}");
                }
                None
            }
        }
    } else {
        None
    };

    loop {
        if stop.load(Ordering::Relaxed) {
            break;
        }
        // ① 回到等待：这里是最容易被写成「只跑一次」的地方
        let session_id = match requests.recv_timeout(Duration::from_secs(1)) {
            Ok(id) => id,
            Err(RecvTimeoutError::Timeout) => continue,
            Err(RecvTimeoutError::Disconnected) => {
                return Err("信令连接中断，请关闭再开启「允许被控」".into());
            }
        };
        if stop.load(Ordering::Relaxed) {
            break;
        }
        // A queued/duplicate notification may have expired while another session was busy.
        if !target
            .session_state(&session_id)
            .is_ok_and(|state| state == "requested")
        {
            continue;
        }
        if let Ok(mut st) = stats.lock() {
            st.last_error.clear();
        }
        // 先接收请求并等待本机点击，再启动会话媒体。无显示器或编码器故障不应切断申请入口。
        let label = format!(
            "远程设备（{}）",
            session_id.chars().take(8).collect::<String>()
        );
        let consent_timeout = std::time::Duration::from_secs(30);
        consent.request(&session_id, &label, consent_timeout);
        match consent.wait_while_pending(stop, consent_timeout, || {
            !target
                .session_state(&session_id)
                .is_ok_and(|state| peer_closed(&state))
        }) {
            Ok(true) => {}
            Ok(false) => {
                if let Err(e) = target.reject(&session_id) {
                    eprintln!("[host] reject failed session_id={session_id}: {e}");
                }
                continue;
            }
            Err(_) if stop.load(Ordering::Relaxed) => {
                if let Err(e) = target.reject(&session_id) {
                    eprintln!("[host] reject on stop failed session_id={session_id}: {e}");
                }
                break;
            }
            Err(_) => continue,
        }
        if stop.load(Ordering::Relaxed) {
            break;
        }

        let result = (|| {
            let (mut cap, mut enc) = open_media(fps)?;
            target.consent(&session_id).map_err(|e| e.to_string())?;
            let (mut relay, mut crypto, msg) =
                target.begin(&session_id).map_err(|e| e.to_string())?;
            let peer_p2p = relay.negotiate_p2p(&P2PConfig::new(
                &session_id,
                p2p_token(&session_id),
                std::time::Duration::from_secs(2),
            ));
            target
                .finish_with_resend(&mut relay, &mut crypto, &session_id, Some(&msg))
                .map_err(|e| e.to_string())?;
            if stop.load(Ordering::Relaxed) {
                return Err("host stopped".into());
            }
            // 初始化后预取一帧；采集错误不应被上报成 ready。静止桌面可能暂时没有新帧。
            let first_frame = enc.capture_and_encode(&mut cap)?;
            target.mark_ready(&session_id).map_err(|e| e.to_string())?;
            Ok::<_, String>((cap, enc, relay, crypto, peer_p2p, first_frame))
        })();
        let (mut cap, mut enc, mut relay, mut crypto, peer_p2p, mut first_frame) = match result {
            Ok(media) => media,
            Err(e) => {
                eprintln!("[host] session setup failed session_id={session_id}: {e}");
                if let Ok(mut st) = stats.lock() {
                    st.last_error = "无法建立远控画面，请检查屏幕录制权限及网络".into();
                }
                if let Err(e) = target.close_session(&session_id) {
                    eprintln!("[host] close after setup failure session_id={session_id}: {e}");
                }
                continue;
            }
        };
        let cfg = enc.config();
        let budget = std::time::Duration::from_millis((1000 / cfg.fps.max(1)) as u64);
        let mut injector = input_injector();
        let mut seq = 0u32;
        if let Ok(mut st) = stats.lock() {
            st.session_id = session_id.clone();
            st.frames = 0;
            st.injected = 0;
            st.link = if peer_p2p { "p2p" } else { "relay" }.into();
        }

        // ③ 推流 + 注入，直到对方离开或 stop
        let mut last_input = std::time::Instant::now();
        // 心跳超时 = 主控真的走了。比 idle 可靠：主控一动不动但还连着时不会误伤。
        let mut last_beat = std::time::Instant::now();
        let beat_timeout = std::time::Duration::from_secs(10);
        let mut last_state_check = std::time::Instant::now();
        let mut input_seq = er_input::InputSequencer::new();
        let mut clipboard = if std::env::var("ER_CLIPBOARD_SYNC").ok().as_deref() == Some("1") {
            Some(er_channels::clipboard::ClipboardService::new(
                er_channels::clipboard::CommandClipboard,
                true,
            ))
        } else {
            None
        };
        let mut last_clip_poll = std::time::Instant::now();
        let mut file_rx: Option<er_channels::file_stream::StreamReceiver> = None;
        let mut remote_closed = false;
        while !stop.load(Ordering::Relaxed) {
            // 关闭传播兜底：对端/后台关闭后 1s 内停止采集与输入，不依赖心跳超时。
            if last_state_check.elapsed() >= std::time::Duration::from_secs(1) {
                last_state_check = std::time::Instant::now();
                if let Ok(state) = target.session_state(&session_id) {
                    if peer_closed(&state) {
                        remote_closed = true;
                        break;
                    }
                }
            }
            if last_beat.elapsed() > beat_timeout {
                break;
            }
            if last_input.elapsed() > idle {
                break;
            }
            let mut drained = 0;
            while drained < 32 {
                match relay.recv_timeout(std::time::Duration::from_millis(2)) {
                    Ok(Some(sealed)) => {
                        if let Ok(plain) = crypto.open(&sealed) {
                            if !plain.is_empty() && plain[0] == crate::agent::MSG_HEARTBEAT {
                                last_beat = std::time::Instant::now();
                            }
                            if !plain.is_empty()
                                && plain[0] == er_channels::file_stream::MSG_FILE_META
                                && std::env::var("ER_FILE_RECV_DIR").is_ok()
                            {
                                file_rx =
                                    er_channels::file_stream::StreamReceiver::from_meta(&plain);
                            }
                            if !plain.is_empty()
                                && plain[0] == er_channels::file_stream::MSG_FILE_CHUNK
                            {
                                if let Some(rx) = file_rx.as_mut() {
                                    let id = rx.id();
                                    if let Ok((index, _)) = rx.on_chunk(&plain) {
                                        let complete = rx.missing().is_empty();
                                        let name = rx.name().to_string();
                                        if !complete {
                                            let ack = er_channels::file_stream::StreamReceiver::ack(
                                                index, id,
                                            );
                                            let sealed = crypto.seal(&ack);
                                            let _ = relay.send(&sealed);
                                        } else {
                                            // 最后一块：先落盘/校验成功，再 ACK，避免发送端误判完成。
                                            let dir = std::env::var("ER_FILE_RECV_DIR")
                                                .unwrap_or_else(|_| ".".into());
                                            let file_name = std::path::Path::new(&name)
                                                .file_name()
                                                .and_then(|s| s.to_str())
                                                .unwrap_or("file.bin");
                                            let path = std::path::Path::new(&dir).join(file_name);
                                            let stored = file_rx.take().is_some_and(|done| {
                                                done.finish().ok().is_some_and(|bytes| {
                                                    let ok = std::fs::write(&path, &bytes).is_ok();
                                                    if ok {
                                                        if let Ok(mut st) = stats.lock() {
                                                            st.last_error = format!(
                                                                "file received: {} ({} bytes)",
                                                                path.display(),
                                                                bytes.len()
                                                            );
                                                        }
                                                    }
                                                    ok
                                                })
                                            });
                                            if stored {
                                                let ack =
                                                    er_channels::file_stream::StreamReceiver::ack(
                                                        index, id,
                                                    );
                                                let sealed = crypto.seal(&ack);
                                                let _ = relay.send(&sealed);
                                            } else if let Ok(mut st) = stats.lock() {
                                                st.last_error =
                                                    "file receive failed: finish/write error"
                                                        .into();
                                            }
                                        }
                                    }
                                }
                            }
                            if !plain.is_empty() && plain[0] == er_channels::clipboard::MSG_CLIP {
                                if let Some(svc) = clipboard.as_mut() {
                                    if let Some(update) =
                                        er_channels::clipboard::decode_clip(&plain)
                                    {
                                        let _ = svc.apply_remote(&update);
                                    }
                                }
                            }
                            if !plain.is_empty() && plain[0] == crate::agent::MSG_KEYFRAME {
                                enc.request_keyframe();
                            }
                            if !plain.is_empty()
                                && plain[0] == crate::agent::MSG_INPUT
                                && plain.len() >= 5
                            {
                                {
                                    let seq = u32::from_be_bytes([
                                        plain[1], plain[2], plain[3], plain[4],
                                    ]);
                                    if let Some(ev) = er_input::InputEvent::decode(&plain[5..]) {
                                        for ordered in input_seq.push(seq, ev) {
                                            if injector.inject(&ordered).is_ok() {
                                                if let Ok(mut st) = stats.lock() {
                                                    st.injected += 1;
                                                }
                                            }
                                        }
                                        last_input = std::time::Instant::now();
                                    }
                                    let mut ack = vec![crate::agent::MSG_INPUT_ACK];
                                    ack.extend_from_slice(&seq.to_be_bytes());
                                    let sealed = crypto.seal(&ack);
                                    let _ = relay.send(&sealed);
                                }
                            }
                        }
                    }
                    _ => break,
                }
                drained += 1;
            }
            if let Some(svc) = clipboard.as_mut() {
                if last_clip_poll.elapsed() >= std::time::Duration::from_secs(1) {
                    last_clip_poll = std::time::Instant::now();
                    if let Ok(Some(update)) = svc.poll_local() {
                        if let Ok(msg) = er_channels::clipboard::encode_clip(&update) {
                            let sealed = crypto.seal(&msg);
                            let _ = relay.send(&sealed);
                        }
                    }
                }
            }
            let t0 = std::time::Instant::now();
            let encoded = match first_frame
                .take()
                .map_or_else(|| enc.capture_and_encode(&mut cap), |frame| Ok(Some(frame)))
            {
                Ok(frame) => frame,
                Err(e) => {
                    eprintln!("[host] capture failed session_id={session_id}: {e}");
                    if let Ok(mut st) = stats.lock() {
                        st.last_error = "屏幕采集失败，请检查本机权限后重新连接".into();
                    }
                    if let Err(e) = target.close_session(&session_id) {
                        eprintln!(
                            "[host] close after capture failure session_id={session_id}: {e}"
                        );
                    }
                    break;
                }
            };
            if let Some(encoded) = encoded.filter(|frame| !frame.is_empty()) {
                seq = seq.wrapping_add(1);
                let hdr = crate::link::VideoHeader {
                    seq,
                    ts_ms: t0.elapsed().as_millis() as u32,
                    width: (cfg.width & 0xffff) as u16,
                    height: (cfg.height & 0xffff) as u16,
                };
                let msg = crate::agent::encode_video_msg(&hdr, &encoded);
                let sealed = crypto.seal(&msg);
                if relay.send(&sealed).is_ok() {
                    if let Ok(mut st) = stats.lock() {
                        st.frames += 1;
                    }
                }
            }
            let spent = t0.elapsed();
            if spent < budget {
                std::thread::sleep(budget - spent);
            }
        }
        if !remote_closed {
            if let Err(e) = target.close_session(&session_id) {
                eprintln!("[host] close after stream end session_id={session_id}: {e}");
            }
        }
        if let Ok(mut st) = stats.lock() {
            st.session_id.clear();
        }
    }

    Ok(())
}

fn open_media(
    fps: u32,
) -> Result<(er_capture::ScreenCapture, crate::encoder::HybridEncoder), String> {
    use er_capture::Capture;
    let cap = er_capture::ScreenCapture::open_primary().map_err(|e| format!("capture: {e}"))?;
    let info = cap.info();
    let fps = fps.clamp(5, 60);
    let requested = er_codec::EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps,
        bitrate_kbps: 4000,
        gop: fps,
        bframes: 0,
    };
    let resolved = er_codec::resolve_config(requested, er_codec::host_capability());
    let enc = crate::encoder::HybridEncoder::new(resolved.config)?;
    Ok((cap, enc))
}

/// 查询被控流状态。
pub fn host_stream_status(host_id: &str) -> HostStreamStats {
    if let Ok(map) = streams().lock() {
        if let Some(h) = map.get(host_id) {
            if let Ok(s) = h.stats.lock() {
                return s.clone();
            }
        }
    }
    HostStreamStats::default()
}

/// 停止被控流（保留 host，可再次 start）。
pub fn stop_host_stream(host_id: &str) {
    if let Ok(mut map) = streams().lock() {
        if let Some(h) = map.remove(host_id) {
            h.stop.store(true, Ordering::Relaxed);
            h.consent.clear();
        }
    }
}

/// UI 轮询：当前等待本机决策的会话。
pub fn pending_consent(host_id: &str) -> Option<ConsentSnapshot> {
    let map = streams().lock().ok()?;
    map.get(host_id)?.consent.pending()
}

/// 本机 UI 的决策入口（FFI）。session 不匹配返回 false。
pub fn decide_consent(host_id: &str, session_id: &str, accepted: bool) -> bool {
    let Ok(map) = streams().lock() else {
        return false;
    };
    let Some(h) = map.get(host_id) else {
        return false;
    };
    h.consent.decide(session_id, accepted)
}

// 会话状态是否意味着「对端已关闭，被控端必须立刻停止数据面」。
//
// 抽成函数的原因：这是 ER-905 的 deadline 收敛判据，必须能被单测直接断言，
// 而不是散落在两处 `if state == ` 里靠读代码相信。
pub fn peer_closed(state: &str) -> bool {
    matches!(state, "closed" | "failed" | "ended")
}

#[cfg(test)]
mod deadline_tests {
    use super::peer_closed;

    /// ER-905：close 后必须立即停止采集/输入（1s 轮询命中即 break）。
    #[test]
    fn peer_closed_state_predicate_matches_close_states() {
        assert!(peer_closed("closed"), "closed 必须触发停止");
        assert!(peer_closed("ended"), "ended 必须触发停止");
        assert!(peer_closed("failed"), "failed 必须触发停止");
        assert!(!peer_closed("authorized"), "authorized 不得停止");
        assert!(!peer_closed("active"), "active 不得停止");
        assert!(!peer_closed("requested"), "requested 不得停止");
        assert!(!peer_closed(""), "空状态不得误停");
    }
}
