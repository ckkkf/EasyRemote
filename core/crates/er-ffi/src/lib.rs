//! C ABI：把 Rust 核心暴露给 Flutter（dart:ffi）。
//!
//! 设计约束：
//!   - 所有跨边界函数不 panic（统一 catch_unwind），错误用负数返回码 + last_error
//!   - 句柄是不透明指针，Dart 侧只持有 `Pointer<Void>`
//!   - 字符串用 `*const c_char`（NUL 结尾），由本库分配、调用方用 `er_free_string` 释放
//!   - 帧数据走调用方提供的 buffer，避免跨边界管理内存

use std::ffi::{c_char, c_int, CString};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::ptr;

mod host;
mod session;
pub use host::{er_device_connect, er_host_prepare, er_host_release, er_host_serve};
pub use session::er_run_pair_session;

use er_abr::{AbrController, LinkFeedback, QualityLevel};
use er_capture::{Capture, RawFrame, ScreenCapture};
use er_client::encoder::HybridEncoder;
use er_codec::EncodeConfig;

/// 返回码。
pub const ER_OK: c_int = 0;
pub const ER_ERR_NULL: c_int = -1;
pub const ER_ERR_NO_DISPLAY: c_int = -2;
pub const ER_ERR_CAPTURE: c_int = -3;
pub const ER_ERR_CODEC: c_int = -4;
pub const ER_ERR_BUFFER_SMALL: c_int = -5;
pub const ER_ERR_PANIC: c_int = -99;

/// 一个会话句柄持有的状态。
pub struct Session {
    capture: Option<ScreenCapture>,
    /// 采集+编码组合器：Windows 上优先 DXGI→NVENC 零拷贝，失败自动回退 CPU
    enc: Option<HybridEncoder>,
    abr: AbrController,
    last_error: CString,
    last_frame: Option<RawFrame>,
    /// 最近一次编码输出，供 encode 接口返回
    last_encoded: Vec<u8>,
}

fn set_err(sess: &mut Session, msg: impl AsRef<str>) {
    sess.last_error = CString::new(msg.as_ref()).unwrap_or_default();
}

/// 把 Rust 字符串移交 Dart（调用方负责 er_free_string）。
fn into_c_string(s: &str) -> *mut c_char {
    CString::new(s).unwrap_or_default().into_raw()
}

/// 释放由本库分配的字符串。
///
/// # Safety
/// 只能传入本库返回的指针，且只能释放一次。
#[no_mangle]
pub unsafe extern "C" fn er_free_string(p: *mut c_char) {
    if !p.is_null() {
        drop(CString::from_raw(p));
    }
}

/// 采集 + 编码一步到位（**GPU 零拷贝优先，失败自动回退 CPU**）。
///
/// 与 er_grab_frame + er_encode_last_frame 的区别：那条路必须把整帧 BGRA 搬到
/// 调用方的 buffer 里，零拷贝就白做了。这条直接在原生侧完成「采集→编码」。
///
/// 返回写入字节数；负数表示错误（含义同 er_encode_last_frame）。
///
/// # Safety
/// buf 必须至少 cap 字节可写。
#[no_mangle]
pub unsafe extern "C" fn er_grab_and_encode(
    sess: *mut Session,
    out_buf: *mut u8,
    cap: usize,
) -> c_int {
    if sess.is_null() || (out_buf.is_null() && cap > 0) {
        return ER_ERR_NULL;
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &mut *sess;
        let Some(capture) = s.capture.as_mut() else {
            set_err(s, "no capture");
            return ER_ERR_NO_DISPLAY;
        };
        let Some(enc) = s.enc.as_mut() else {
            set_err(s, "no encoder");
            return ER_ERR_CODEC;
        };
        match enc.capture_and_encode(capture) {
            Ok(Some(data)) => {
                if data.len() > cap {
                    set_err(
                        s,
                        format!("buffer too small: need {}, have {cap}", data.len()),
                    );
                    return ER_ERR_BUFFER_SMALL;
                }
                ptr::copy_nonoverlapping(data.as_ptr(), out_buf, data.len());
                s.last_encoded = data.clone();
                data.len() as c_int
            }
            Ok(None) => {
                set_err(s, "no new frame");
                ER_ERR_CAPTURE
            }
            Err(e) => {
                set_err(s, format!("grab+encode: {e}"));
                ER_ERR_CAPTURE
            }
        }
    }));
    r.unwrap_or(ER_ERR_PANIC)
}

/// 当前实际生效的采集/编码后端："gpu-zero-copy" 或 "cpu"。
///
/// 返回值由本库分配，调用方用 er_free_string 释放。
///
/// # Safety
/// 指针需为 er_session_new 返回的句柄（可为 null，返回空串）。
#[no_mangle]
pub unsafe extern "C" fn er_encoder_backend(sess: *mut Session) -> *mut c_char {
    let name = catch_unwind(AssertUnwindSafe(|| {
        if sess.is_null() {
            return String::new();
        }
        let s = &*sess;
        s.enc
            .as_ref()
            .map(|e| e.backend().to_string())
            .unwrap_or_default()
    }))
    .unwrap_or_default();
    into_c_string(&name)
}

/// 本机是否有硬件编码器（Windows/NVIDIA 才有意义；其它平台恒 0）。
///
/// 判定标准是**真能开出一次 NVENC 会话**，不是"看见显卡名字"。
#[no_mangle]
pub extern "C" fn er_hardware_encoder_available() -> c_int {
    catch_unwind(AssertUnwindSafe(|| {
        er_codec::hardware_encoder_available() as c_int
    }))
    .unwrap_or(0)
}

/// 当前会话实际使用的编码器名："nvenc" / "software" / ""（无会话）。
///
/// 返回的字符串由本库分配，调用方用 er_free_string 释放。
///
/// # Safety
/// 指针需为 er_session_new 返回的句柄（可为 null，返回空串）。
#[no_mangle]
pub unsafe extern "C" fn er_encoder_kind(sess: *mut Session) -> *mut c_char {
    let name = catch_unwind(AssertUnwindSafe(|| {
        if sess.is_null() {
            return String::new();
        }
        let s = &*sess;
        match &s.enc {
            Some(c) => match c.cpu_kind() {
                er_codec::EncoderKind::Nvenc => "nvenc".to_string(),
                er_codec::EncoderKind::Software => "software".to_string(),
                other => format!("{other:?}").to_lowercase(),
            },
            None => String::new(),
        }
    }))
    .unwrap_or_default();
    into_c_string(&name)
}

/// 版本号。
#[no_mangle]
pub extern "C" fn er_version() -> *mut c_char {
    into_c_string(env!("CARGO_PKG_VERSION"))
}

/// 创建会话。返回不透明句柄；失败返回 null。
#[no_mangle]
pub extern "C" fn er_session_new() -> *mut Session {
    let result = catch_unwind(AssertUnwindSafe(|| {
        let mut sess = Session {
            capture: None,
            enc: None,
            abr: AbrController::new(0),
            last_error: CString::new("").unwrap(),
            last_frame: None,
            last_encoded: Vec::new(),
        };
        match ScreenCapture::open_primary() {
            Ok(cap) => {
                let info = cap.info();
                let cfg = EncodeConfig {
                    width: info.width & !1,
                    height: info.height & !1,
                    fps: 60,
                    bitrate_kbps: 4000,
                    gop: 60,
                    bframes: 0,
                };
                match HybridEncoder::new(cfg) {
                    Ok(e) => sess.enc = Some(e),
                    Err(e) => set_err(&mut sess, format!("codec init: {e}")),
                }
                sess.capture = Some(cap);
            }
            Err(e) => set_err(&mut sess, format!("capture init: {e}")),
        }
        Box::into_raw(Box::new(sess))
    }));
    result.unwrap_or(ptr::null_mut())
}

/// 销毁会话。
///
/// # Safety
/// 只能传入 er_session_new 返回且尚未销毁的指针。
#[no_mangle]
pub unsafe extern "C" fn er_session_free(p: *mut Session) {
    if !p.is_null() {
        drop(Box::from_raw(p));
    }
}

/// 屏幕尺寸写入 out_w/out_h。返回 ER_OK。
///
/// # Safety
/// out_w/out_h 必须是有效可写指针。
#[no_mangle]
pub unsafe extern "C" fn er_screen_size(
    sess: *mut Session,
    out_w: *mut u32,
    out_h: *mut u32,
) -> c_int {
    if sess.is_null() || out_w.is_null() || out_h.is_null() {
        return ER_ERR_NULL;
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &*sess;
        match &s.capture {
            Some(c) => {
                let info = c.info();
                *out_w = info.width;
                *out_h = info.height;
                ER_OK
            }
            None => ER_ERR_NO_DISPLAY,
        }
    }));
    r.unwrap_or(ER_ERR_PANIC)
}

/// 抓一帧到调用方的 buffer（BGRA8）。
///
/// 返回写入字节数；负数表示错误。
/// buffer 不足时返回 ER_ERR_BUFFER_SMALL，并需用 er_frame_size 预先查询。
///
/// # Safety
/// buf 必须至少 cap 字节可写。
#[no_mangle]
pub unsafe extern "C" fn er_grab_frame(sess: *mut Session, buf: *mut u8, cap: usize) -> c_int {
    if sess.is_null() || (buf.is_null() && cap > 0) {
        return ER_ERR_NULL;
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &mut *sess;
        let Some(cap_ref) = s.capture.as_mut() else {
            return ER_ERR_NO_DISPLAY;
        };
        let frame = match cap_ref.grab() {
            Ok(f) => f,
            Err(e) => {
                set_err(s, format!("grab: {e}"));
                return ER_ERR_CAPTURE;
            }
        };
        let need = frame.data.len();
        if need > cap {
            set_err(s, format!("buffer too small: need {need}, have {cap}"));
            return ER_ERR_BUFFER_SMALL;
        }
        ptr::copy_nonoverlapping(frame.data.as_ptr(), buf, need);
        s.last_frame = Some(frame);
        need as c_int
    }));
    r.unwrap_or(ER_ERR_PANIC)
}

/// 一帧需要的字节数。
///
/// # Safety
/// 指针需有效。
#[no_mangle]
pub unsafe extern "C" fn er_frame_size(sess: *mut Session) -> c_int {
    if sess.is_null() {
        return ER_ERR_NULL;
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &*sess;
        match &s.capture {
            Some(c) => {
                let info = c.info();
                ((info.width & !1) * (info.height & !1) * 4) as c_int
            }
            None => ER_ERR_NO_DISPLAY,
        }
    }));
    r.unwrap_or(ER_ERR_PANIC)
}

/// 编码上一帧，返回写入 out_buf 的字节数（负数=错误）。
///
/// # Safety
/// out_buf 至少 cap 字节可写。
#[no_mangle]
pub unsafe extern "C" fn er_encode_last_frame(
    sess: *mut Session,
    out_buf: *mut u8,
    cap: usize,
) -> c_int {
    if sess.is_null() || (out_buf.is_null() && cap > 0) {
        return ER_ERR_NULL;
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &mut *sess;
        let Some(frame) = s.last_frame.clone() else {
            set_err(s, "no frame captured yet");
            return ER_ERR_CAPTURE;
        };
        let Some(enc) = s.enc.as_mut() else {
            return ER_ERR_CODEC;
        };
        match enc.encode_bgra(frame.ts_ms, &frame.data) {
            Ok(data) => {
                if data.len() > cap {
                    set_err(
                        s,
                        format!("buffer too small: need {}, have {cap}", data.len()),
                    );
                    return ER_ERR_BUFFER_SMALL;
                }
                ptr::copy_nonoverlapping(data.as_ptr(), out_buf, data.len());
                s.last_encoded = data.clone();
                data.len() as c_int
            }
            Err(e) => {
                set_err(s, format!("encode: {e}"));
                ER_ERR_CODEC
            }
        }
    }));
    r.unwrap_or(ER_ERR_PANIC)
}

/// 喂一次链路反馈，返回当前档位 JSON（调用方释放字符串）。
///
/// # Safety
/// 指针需有效。
#[no_mangle]
pub unsafe extern "C" fn er_abr_update(
    sess: *mut Session,
    loss_pct: f32,
    rtt_ms: u32,
    queue_ms: u32,
) -> *mut c_char {
    if sess.is_null() {
        return ptr::null_mut();
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &mut *sess;
        let d = s.abr.on_feedback(&LinkFeedback {
            loss_pct,
            rtt_ms,
            queue_ms,
            throughput_kbps: 0,
            buffer_ms: queue_ms,
            encode_queue: 0,
        });
        let lvl: QualityLevel = d.level;
        into_c_string(&format!(
            "{{\"name\":\"{}\",\"bitrate_kbps\":{},\"fps\":{},\"downscale\":{},\"changed\":{},\"reason\":\"{}\"}}",
            lvl.name, lvl.bitrate_kbps, lvl.fps, lvl.downscale, d.changed, d.reason
        ))
    }));
    r.unwrap_or(ptr::null_mut())
}

/// 最近一次错误信息（调用方释放）。
///
/// # Safety
/// 指针需有效。
#[no_mangle]
pub unsafe extern "C" fn er_last_error(sess: *mut Session) -> *mut c_char {
    if sess.is_null() {
        return into_c_string("null session");
    }
    let r = catch_unwind(AssertUnwindSafe(|| {
        let s = &*sess;
        let msg = s.last_error.to_str().unwrap_or("").to_string();
        into_c_string(&msg)
    }));
    r.unwrap_or(ptr::null_mut())
}

#[cfg(test)]
mod tests;
