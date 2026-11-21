//! 主机 / 主控两段式会话的 C ABI（v2：主控只用设备码 + 验证码）。

use std::ffi::{c_char, CStr};
use std::panic::{catch_unwind, AssertUnwindSafe};

use er_client::conductor::{connect_device, prepare_host, release_host, serve_host};

use crate::into_c_string;
use crate::session::escape_json;

unsafe fn cstr(p: *const c_char) -> Option<String> {
    if p.is_null() {
        None
    } else {
        Some(CStr::from_ptr(p).to_string_lossy().into_owned())
    }
}

/// 准备主机，返回 {"ok":true,"host_id":..,"device_code":..,"captcha":..}。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_prepare(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    phone: *const c_char,
    sms_code: *const c_char,
) -> *mut c_char {
    let (Some(gw), Some(relay), Some(phone), Some(code)) = (
        cstr(gateway_url),
        cstr(relay_addr),
        cstr(phone),
        cstr(sms_code),
    ) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        let addr = match relay.parse() {
            Ok(a) => a,
            Err(e) => {
                return into_c_string(&format!("{{\"ok\":false,\"error\":\"bad relay: {e}\"}}"))
            }
        };
        match prepare_host(&gw, addr, &phone, &code) {
            Ok((host_id, device_code, captcha)) => into_c_string(&format!(
                "{{\"ok\":true,\"host_id\":\"{}\",\"device_code\":\"{}\",\"captcha\":\"{}\"}}",
                escape_json(&host_id),
                escape_json(&device_code),
                escape_json(&captcha)
            )),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 主机侧服务：阻塞等待信令 → 授权 → 收帧。应在后台 isolate 调用。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_serve(
    host_id: *const c_char,
    signal_base: *const c_char,
    frames: u32,
    timeout_secs: u32,
) -> *mut c_char {
    let (Some(hid), Some(sig)) = (cstr(host_id), cstr(signal_base)) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        match serve_host(
            &hid,
            &sig,
            frames.max(1) as usize,
            timeout_secs.max(1) as u64,
        ) {
            Ok(res) => into_c_string(&format!(
                "{{\"ok\":true,\"session_id\":\"{}\",\"frames\":{},\"width\":{},\"height\":{}}}",
                escape_json(&res.session_id),
                res.frames.len(),
                res.frames.first().map(|f| f.width).unwrap_or(0),
                res.frames.first().map(|f| f.height).unwrap_or(0)
            )),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 主控连接：只用设备码 + 验证码。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_device_connect(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    phone: *const c_char,
    device_code: *const c_char,
    captcha: *const c_char,
    sms_code: *const c_char,
    frames: u32,
) -> *mut c_char {
    let (Some(gw), Some(relay), Some(phone), Some(code), Some(cap), Some(sms)) = (
        cstr(gateway_url),
        cstr(relay_addr),
        cstr(phone),
        cstr(device_code),
        cstr(captcha),
        cstr(sms_code),
    ) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        let addr = match relay.parse() {
            Ok(a) => a,
            Err(e) => {
                return into_c_string(&format!("{{\"ok\":false,\"error\":\"bad relay: {e}\"}}"))
            }
        };
        match connect_device(&gw, addr, &phone, &code, &cap, &sms, frames.max(1) as usize) {
            Ok(res) => into_c_string(&format!(
                "{{\"ok\":true,\"session_id\":\"{}\"}}",
                escape_json(&res.session_id)
            )),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 释放主机。
///
/// # Safety
/// host_id 必须是以 NUL 结尾的字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_release(host_id: *const c_char) -> i32 {
    match cstr(host_id) {
        Some(id) => i32::from(release_host(&id)),
        None => 0,
    }
}
