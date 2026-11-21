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
    let cstr = |p: *const c_char| -> String {
        CStr::from_ptr(p).to_string_lossy().into_owned()
    };
    let gw = cstr(gateway_url);
    let relay = cstr(relay_addr);
    let pa = cstr(phone_a);
    let pb = cstr(phone_b);
    let code = cstr(sms_code);

    let result = catch_unwind(AssertUnwindSafe(|| {
        let addr = match relay.parse() {
            Ok(a) => a,
            Err(e) => return into_c_string(&format!("{{\"ok\":false,\"error\":\"bad relay addr: {e}\"}}")),
        };
        match run_pair(&gw, addr, &pa, &pb, &code) {
            Ok(report) => {
                let json = format!(
                    "{{\"ok\":true,\"session_id\":\"{}\",\"encoded_bytes\":{},\"sealed_bytes\":{},\"width\":{},\"height\":{},\"keys_match\":{},\"handshake_verified\":{},\"plaintext_absent\":{}}}",
                    report.session_id,
                    report.encoded_bytes,
                    report.sealed_bytes,
                    report.decoded_width,
                    report.decoded_height,
                    report.keys_match_registered,
                    report.handshake_verified,
                    report.plaintext_marker_absent
                );
                into_c_string(&json)
            }
            Err(e) => into_c_string(&format!("{{\"ok\":false,\"error\":\"{}\"}}", escape_json(&e))),
        }
    }));
    result.unwrap_or_else(|_| into_c_string("{\"ok\":false,\"error\":\"panic\"}"))
}

pub fn escape_json(s: &str) -> String {
    s.replace('\\', "\\\\").replace('"', "\\\"").replace('\n', " ")
}
