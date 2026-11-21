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

/// 设置本机「临时密码」（被控端本地生成；服务端只存哈希，校验不消费）。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_set_password(
    host_id: *const c_char,
    password: *const c_char,
) -> *mut c_char {
    let (Some(hid), Some(pw)) = (cstr(host_id), cstr(password)) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| match set_host_password(&hid, &pw) {
        Ok(()) => into_c_string("{\"ok\":true}"),
        Err(e) => into_c_string(&format!(
            "{{\"ok\":false,\"error\":\"{}\"}}",
            escape_json(&e)
        )),
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 启动流式被控（后台线程：等主控→授权→握手→持续推流 + 注入）。
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_start_stream(
    host_id: *const c_char,
    signal_base: *const c_char,
    fps: u32,
    idle_timeout_secs: u64,
) -> *mut c_char {
    let (Some(hid), Some(sig)) = (cstr(host_id), cstr(signal_base)) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(
        || match er_client::host_stream::start_host_stream(&hid, &sig, fps, idle_timeout_secs) {
            Ok(()) => into_c_string("{\"ok\":true}"),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        },
    ));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 查询流式被控状态：{"running":bool,"session_id":..,"frames":n,"injected":n,"error":..}
///
/// # Safety
/// host_id 必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_stream_status(host_id: *const c_char) -> *mut c_char {
    let Some(hid) = cstr(host_id) else {
        return into_c_string("{\"running\":false,\"error\":\"null argument\"}");
    };
    let st = er_client::host_stream::host_stream_status(&hid);
    into_c_string(&format!(
        "{{\"running\":{},\"session_id\":\"{}\",\"frames\":{},\"injected\":{},\"error\":\"{}\"}}",
        st.running,
        escape_json(&st.session_id),
        st.frames,
        st.injected,
        escape_json(&st.last_error)
    ))
}

/// 停止流式被控（保留 host 可再次启动）。
///
/// # Safety
/// host_id 必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_stop_stream(host_id: *const c_char) -> i32 {
    match cstr(host_id) {
        Some(hid) => {
            er_client::host_stream::stop_host_stream(&hid);
            1
        }
        None => 0,
    }
}

/// 从注册表取 host 并设置临时密码。
fn set_host_password(host_id: &str, password: &str) -> Result<(), String> {
    let (target, _relay) = er_client::conductor::host_target(host_id)?;
    target.set_password(password).map_err(|e| e.to_string())
}

/// 读取/生成被控端临时密码并同步到服务端，返回 {"ok":true,"password":"..."}。
///
/// # Safety
/// host_id 必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_ensure_password(host_id: *const c_char) -> *mut c_char {
    let Some(hid) = cstr(host_id) else {
        return into_c_string("{\"ok\":false,\"error\":\"null argument\"}");
    };
    let r = catch_unwind(AssertUnwindSafe(|| {
        let pw = er_client::conductor::ensure_host_password();
        match set_host_password(&hid, &pw) {
            Ok(()) => into_c_string(&format!(
                "{{\"ok\":true,\"password\":\"{}\"}}",
                escape_json(&pw)
            )),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

/// 用已登录会话准备主机：不重复登录/绑设备，设备码与客户端「我的设备」一致。
///
/// 参数：gateway, relay, token, device_id, device_code, device_token
/// 返回：{"ok":true,"host_id":"..","device_code":".."}
///
/// # Safety
/// 参数必须是以 NUL 结尾的 UTF-8 字符串。
#[no_mangle]
pub unsafe extern "C" fn er_host_prepare_ex(
    gateway_url: *const c_char,
    relay_addr: *const c_char,
    token: *const c_char,
    device_id: *const c_char,
    device_code: *const c_char,
    device_token: *const c_char,
) -> *mut c_char {
    let (Some(gw), Some(relay), Some(tok), Some(did), Some(dcode), Some(dtok)) = (
        cstr(gateway_url),
        cstr(relay_addr),
        cstr(token),
        cstr(device_id),
        cstr(device_code),
        cstr(device_token),
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
        match er_client::conductor::prepare_host_from_session(&gw, addr, &tok, &did, &dcode, &dtok)
        {
            Ok((host_id, code)) => into_c_string(&format!(
                "{{\"ok\":true,\"host_id\":\"{}\",\"device_code\":\"{}\"}}",
                escape_json(&host_id),
                escape_json(&code)
            )),
            Err(e) => into_c_string(&format!(
                "{{\"ok\":false,\"error\":\"{}\"}}",
                escape_json(&e)
            )),
        }
    }));
    r.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}
