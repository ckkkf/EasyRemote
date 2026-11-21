
// ============================================================================
// 流式被控：给「同一个客户端既当主控又当被控」用。
//
// 与 serve_host 的区别：serve_host 是"收 N 帧就返回"的一次性验证；这里是产品形态——
// 挂在那儿等人连，连上后持续推流 + 实时注入鼠标键盘，直到主动停止或对端离开。
// ============================================================================

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

/// 被控流的运行状态（供 UI 轮询）。
#[derive(Default, Clone)]
pub struct HostStreamStats {
    pub running: bool,
    pub session_id: String,
    pub frames: u64,
    pub injected: u64,
    pub last_error: String,
}

struct HostStreamHandle {
    stop: Arc<AtomicBool>,
    stats: Arc<std::sync::Mutex<HostStreamStats>>,
}

fn streams() -> &'static std::sync::Mutex<std::collections::HashMap<String, HostStreamHandle>> {
    use std::sync::OnceLock;
    static S: OnceLock<std::sync::Mutex<std::collections::HashMap<String, HostStreamHandle>>> =
        OnceLock::new();
    S.get_or_init(|| std::sync::Mutex::new(std::collections::HashMap::new()))
}
/// 启动后台被控流：等待主控 → 授权 → 握手 → 循环（收输入注入 + 采集推流）。
///
/// 密码由调用方负责（`TargetAgent::set_password`），这里只管会话与画面/输入。
pub fn start_host_stream(
    host_id: &str,
    signal_base: &str,
    fps: u32,
    idle_timeout_secs: u64,
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
    if let Ok(mut s) = stats.lock() {
        s.running = true;
    }
    streams()
        .lock()
        .map_err(|_| "stream registry poisoned".to_string())?
        .insert(
            host_id.to_string(),
            HostStreamHandle { stop: stop.clone(), stats: stats.clone() },
        );

    let signal = signal_base.to_string();
    let stats_thread = stats.clone();
    std::thread::spawn(move || {
        let result = run_host_stream(&target, &signal, fps, idle_timeout_secs, &stop, &stats_thread);
        if let Err(e) = result {
            if let Ok(mut s) = stats_thread.lock() {
                s.last_error = e;
            }
        }
        if let Ok(mut s) = stats_thread.lock() {
            s.running = false;
        }
    });

    Ok(())
}
fn run_host_stream(
    target: &crate::agent::TargetAgent,
    signal: &str,
    fps: u32,
    idle_timeout_secs: u64,
    stop: &Arc<AtomicBool>,
    stats: &Arc<std::sync::Mutex<HostStreamStats>>,
) -> Result<(), String> {
    let session_id = target
        .wait_for_session(signal, std::time::Duration::from_secs(3600))
        .map_err(|e| e.to_string())?;
    target.consent(&session_id).map_err(|e| e.to_string())?;
    if let Ok(mut s) = stats.lock() {
        s.session_id = session_id.clone();
    }
    std::thread::sleep(std::time::Duration::from_millis(1200));
    let (mut relay, mut crypto, msg) = target.begin(&session_id).map_err(|e| e.to_string())?;
    std::thread::sleep(std::time::Duration::from_millis(2500));
    target.announce(&mut relay, &msg).map_err(|e| e.to_string())?;
    target
        .finish(&mut relay, &mut crypto, &session_id)
        .map_err(|e| e.to_string())?;

    let mut cap = er_capture::ScreenCapture::open_primary().map_err(|e| format!("capture: {e}"))?;
    let info = er_capture::Capture::info(&cap);
    let fps = fps.clamp(5, 60);
    let cfg = er_codec::EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps,
        bitrate_kbps: 4000,
        gop: fps,
        bframes: 0,
    };
    let mut enc = crate::encoder::HybridEncoder::new(cfg)?;
    let mut injector = input_injector();
    let budget = std::time::Duration::from_millis((1000 / fps.max(1)) as u64);
    let mut seq: u32 = 0;
    let mut last_input = std::time::Instant::now();
    let idle = std::time::Duration::from_secs(idle_timeout_secs.max(60));

    while !stop.load(Ordering::Relaxed) {
        if last_input.elapsed() > idle {
            if let Ok(mut s) = stats.lock() {
                s.last_error = "peer idle timeout".to_string();
            }
            break;
        }
        let mut drained = 0;
        while drained < 32 {
            match relay.recv_timeout(std::time::Duration::from_millis(2)) {
                Ok(Some(sealed)) => {
                    if let Ok(plain) = crypto.open(&sealed) {
                        if !plain.is_empty() && plain[0] == crate::agent::MSG_INPUT {
                            if let Some(ev) = er_input::InputEvent::decode(&plain[1..]) {
                                if injector.inject(&ev).is_ok() {
                                    if let Ok(mut s) = stats.lock() {
                                        s.injected += 1;
                                    }
                                }
                                last_input = std::time::Instant::now();
                            }
                        }
                    }
                }
                _ => break,
            }
            drained += 1;
        }
        let t0 = std::time::Instant::now();
        if let Ok(Some(encoded)) = enc.capture_and_encode(&mut cap) {
            if !encoded.is_empty() {
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
                    if let Ok(mut s) = stats.lock() {
                        s.frames += 1;
                    }
                }
            }
        }
        let spent = t0.elapsed();
        if spent < budget {
            std::thread::sleep(budget - spent);
        }
    }
    Ok(())
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
        }
    }
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
