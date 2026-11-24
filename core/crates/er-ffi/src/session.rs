//! 会话编排的 C ABI：让 Flutter 能发起一次真实会话并拿到结果。

use std::ffi::{c_char, CStr};
use std::panic::{catch_unwind, AssertUnwindSafe};

use er_client::pair::run_pair;

use crate::into_c_string;

/// 跑一次完整会话（本机双端）。返回 JSON 字符串，调用方用 er_free_string 释放。
///
/// 生产形态应由两端分别调用；这里的单进程编排用于自检与联调。
///
/// # Safety
/// 四个指针参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_run_pair_session(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    phone_a: *const c_char,
    phone_b: *const c_char,
    sms_code: *const c_char,
) -> *mut c_char {
    if gateway_url.is_null()
        || relay_addr.is_null()
        || phone_a.is_null()
        || phone_b.is_null()
        || sms_code.is_null()
    {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    }
    let cstr = |p: *const c_char| -> String { CStr::from_ptr(p).to_string_lossy().into_owned() };
    let gw = cstr(gateway_url);
    let relay = cstr(relay_addr);
    let pa = cstr(phone_a);
    let pb = cstr(phone_b);
    let code = cstr(sms_code);

    let result = catch_unwind(AssertUnwindSafe(|| {
        let addr = match relay.parse() {
            Ok(a) => a,
            Err(e) => {
                return into_c_string(&format!(
                    "{{\"ok\":false,\"error\":\"bad relay addr: {e}\"}}"
                ))
            }
        };
        match run_pair(&gw, addr, &pa, &pb, &code) {
            Ok(report) => {
                let json = format!(
                    "{{\"ok\":true,\"session_id\":\"{}\",\"encoded_bytes\":{},\"sealed_bytes\":{},\"width\":{},\"height\":{},\"keys_match\":{},\"handshake_verified\":{},\"plaintext_absent\":{},\"link\":\"{}\"}}",
                    report.session_id,
                    report.encoded_bytes,
                    report.sealed_bytes,
                    report.decoded_width,
                    report.decoded_height,
                    report.keys_match_registered,
                    report.handshake_verified,
                    report.plaintext_marker_absent,
                    report.link
                );
                into_c_string(&json)
            }
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    result.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

pub fn escape_json(s: &str) -> String {
    s.replace('\\', "\\\\")
        .replace('"', "\\\"")
        .replace('\n', " ")
}

// ---------------------------------------------------------------------------
// 主控会话（客户端渲染用）：connect → 轮询 frame → 发 input → close
// ---------------------------------------------------------------------------

use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

use er_client::ctrl_session::CtrlSession;

fn ctrl_registry() -> &'static Mutex<HashMap<String, Arc<CtrlSession>>> {
    static R: OnceLock<Mutex<HashMap<String, Arc<CtrlSession>>>> = OnceLock::new();
    R.get_or_init(|| Mutex::new(HashMap::new()))
}

/// 发起主控会话。返回 {"ok":true,"session_id":".."}。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_connect(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    phone: *const c_char,
    sms_code: *const c_char,
    device_code: *const c_char,
    password: *const c_char,
) -> *mut c_char {
    let (Some(gw), Some(relay), Some(ph), Some(sms), Some(code), Some(pw)) = (
        cstr(gateway_url),
        cstr(relay_addr),
        cstr(phone),
        cstr(sms_code),
        cstr(device_code),
        cstr(password),
    ) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        let addr: std::net::SocketAddr = match relay.parse() {
            Ok(a) => a,
            Err(e) => {
                return into_c_string(&format!("{{\"ok\":false,\"error\":\"bad relay: {e}\"}}"));
            }
        };
        match er_client::ctrl_session::connect(&gw, addr, &ph, &sms, &code, &pw) {
            Ok(sess) => {
                let sid = sess.id.clone();
                if let Ok(mut reg) = ctrl_registry().lock() {
                    reg.insert(sid.clone(), Arc::new(sess));
                }
                into_c_string(&format!(
                    "{{\"ok\":true,\"session_id\":\"{}\"}}",
                    escape_json(&sid)
                ))
            }
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":{{\"code\":\"{}\",\"name\":\"{}\",\"message\":\"{}\",\"retryable\":{},\"trace_id\":\"{}\"}}}}",
                escape_json(&e.code),
                escape_json(&e.name),
                escape_json(&e.message),
                e.retryable,
                escape_json(&e.trace_id)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 会话状态：{"connected":bool,"frames":n,"width":w,"height":h,"error":".."}
///
/// # Safety
/// session_id 必须是以 NUL 结尾的字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_info(session_id: *const c_char) -> *mut c_char {
    let Some(sid) = cstr(session_id) else {
        return into_c_string("{\"connected\":false,\"error\":\"null argument\"}");
    };
    let sess = ctrl_registry()
        .lock()
        .ok()
        .and_then(|r| r.get(&sid).cloned());
    let Some(sess) = sess else {
        return into_c_string("{\"connected\":false,\"error\":\"unknown session\"}");
    };
    let (connected, frames, fps, decode_ms, dropped, bitrate, loss, input_acks, input_p95, err) =
        sess.stats
            .lock()
            .map(|s| {
                (
                    s.connected,
                    s.frames,
                    s.decode_fps,
                    s.decode_ms,
                    s.dropped_frames,
                    s.bitrate_kbps,
                    s.loss_pct,
                    s.input_acks,
                    s.input_p95_ms,
                    s.last_error.clone(),
                )
            })
            .unwrap_or((
                false,
                0,
                0,
                0,
                0,
                0,
                0.0,
                0,
                0,
                "stats poisoned".to_string(),
            ));
    let (w, h) = sess
        .latest
        .lock()
        .ok()
        .and_then(|g| g.as_ref().map(|f| (f.width, f.height)))
        .unwrap_or((0, 0));
    into_c_string(&format!(
        "{{\"connected\":{},\"frames\":{},\"fps\":{},\"decode_ms\":{},\"dropped\":{},\"bitrate_kbps\":{},\"loss_pct\":{:.1},\"input_acks\":{},\"input_p95_ms\":{},\"width\":{},\"height\":{},\"error\":\"{}\"}}",
        connected,
        frames,
        fps,
        decode_ms,
        dropped,
        bitrate,
        loss,
        input_acks,
        input_p95,
        w,
        h,
        escape_json(&err)
    ))
}

/// 取最新一帧 RGBA 到调用方缓冲。返回写入字节数；0 = 暂无新帧；负数 = 错误。
///
/// # Safety
/// buf 必须可写 cap 字节；session_id 必须是以 NUL 结尾的字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_frame(session_id: *const c_char, buf: *mut u8, cap: usize) -> i32 {
    let Some(sid) = cstr(session_id) else {
        return -1;
    };
    if buf.is_null() {
        return -1;
    }
    let sess = ctrl_registry()
        .lock()
        .ok()
        .and_then(|r| r.get(&sid).cloned());
    let Some(sess) = sess else { return -2 };
    let Some(frame) = sess.snapshot() else {
        return 0;
    };
    let need = frame.rgba.len();
    if cap < need {
        return -(need as i32);
    }
    std::ptr::copy_nonoverlapping(frame.rgba.as_ptr(), buf, need);
    need as i32
}

/// 发送一个输入事件。kind: 0=move(x,y) 1=button(btn,pressed) 2=wheel(dx,dy) 3=key(scan,pressed)
///
/// # Safety
/// session_id 必须是以 NUL 结尾的字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_input(
    session_id: *const c_char,
    kind: i32,
    a: i32,
    b: i32,
) -> i32 {
    let Some(sid) = cstr(session_id) else {
        return -1;
    };
    let sess = ctrl_registry()
        .lock()
        .ok()
        .and_then(|r| r.get(&sid).cloned());
    let Some(sess) = sess else { return -2 };
    let ev = match kind {
        0 => er_input::InputEvent::MouseMove { x: a, y: b },
        1 => er_input::InputEvent::MouseButton {
            button: match a {
                1 => er_input::MouseButton::Right,
                2 => er_input::MouseButton::Middle,
                _ => er_input::MouseButton::Left,
            },
            pressed: b != 0,
        },
        2 => er_input::InputEvent::Wheel { dx: a, dy: b },
        3 => er_input::InputEvent::Key {
            scancode: a as u16,
            pressed: b != 0,
        },
        _ => return -3,
    };
    sess.send_input(ev);
    0
}

/// 关闭会话。
///
/// # Safety
/// session_id 必须是以 NUL 结尾的字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_close(session_id: *const c_char) -> i32 {
    let Some(sid) = cstr(session_id) else {
        return 0;
    };
    let removed = ctrl_registry().lock().ok().and_then(|mut r| r.remove(&sid));
    match removed {
        Some(s) => {
            s.stop();
            1
        }
        None => 0,
    }
}

/// 读 C 字符串（带空指针保护）。
unsafe fn cstr(p: *const c_char) -> Option<String> {
    if p.is_null() {
        None
    } else {
        Some(CStr::from_ptr(p).to_string_lossy().into_owned())
    }
}

/// 接管已建立的会话（客户端 UI 层已 request，这里只跑收发）。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_ctrl_attach(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    token: *const c_char,
    device_id: *const c_char,
    device_token: *const c_char,
    session_id: *const c_char,
) -> *mut c_char {
    let (Some(gw), Some(relay), Some(tok), Some(did), Some(dtok), Some(sid)) = (
        cstr(gateway_url),
        cstr(relay_addr),
        cstr(token),
        cstr(device_id),
        cstr(device_token),
        cstr(session_id),
    ) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        let addr: std::net::SocketAddr = match relay.parse() {
            Ok(a) => a,
            Err(e) => {
                return into_c_string(&format!("{{\"ok\":false,\"error\":\"bad relay: {e}\"}}"));
            }
        };
        match er_client::ctrl_session::attach(&gw, addr, &tok, &did, &dtok, &sid) {
            Ok(sess) => {
                let id = sess.id.clone();
                if let Ok(mut reg) = ctrl_registry().lock() {
                    reg.insert(id.clone(), Arc::new(sess));
                }
                into_c_string(&format!(
                    "{{\"ok\":true,\"session_id\":\"{}\"}}",
                    escape_json(&id)
                ))
            }
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":{{\"code\":\"{}\",\"name\":\"{}\",\"message\":\"{}\",\"retryable\":{},\"trace_id\":\"{}\"}}}}",
                escape_json(&e.code),
                escape_json(&e.name),
                escape_json(&e.message),
                e.retryable,
                escape_json(&e.trace_id)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}
