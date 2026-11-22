//! C ABI 的 Rust 侧自检（不依赖 Dart）。

use super::*;
use std::ffi::CStr;

#[test]
fn null_pointers_are_checked() {
    unsafe {
        assert_eq!(er_frame_size(std::ptr::null_mut()), ER_ERR_NULL);
        assert_eq!(
            er_grab_frame(std::ptr::null_mut(), std::ptr::null_mut(), 0),
            ER_ERR_NULL
        );
    }
}

#[test]
fn string_roundtrip_frees_cleanly() {
    unsafe {
        let p = er_version();
        assert!(!p.is_null());
        let v = CStr::from_ptr(p).to_str().unwrap().to_string();
        assert!(!v.is_empty());
        er_free_string(p);
    }
}

#[test]
fn session_lifecycle_does_not_crash() {
    let s = er_session_new();
    if s.is_null() {
        return; // 极端情况下分配失败
    }
    unsafe {
        let err = er_last_error(s);
        assert!(!err.is_null());
        er_free_string(err);
        er_session_free(s);
    }
}
