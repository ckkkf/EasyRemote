//! C ABI 的 Rust 侧自检（不依赖 Dart）。

use super::*;
use std::ffi::CStr;

#[test]
fn privacy_scrub_flattens_buffer() {
    let mut buf = vec![0xABu8; 64];
    unsafe {
        let rc = er_privacy_scrub(buf.as_mut_ptr(), buf.len());
        assert_eq!(rc, ER_OK);
    }
    assert!(buf.chunks_exact(4).all(|px| px == [0, 0, 0, 255]));
}

#[test]
fn privacy_scrub_rejects_bad_input() {
    unsafe {
        assert_eq!(er_privacy_scrub(std::ptr::null_mut(), 4), ER_ERR_NULL);
        let mut buf = vec![0u8; 5];
        assert_eq!(er_privacy_scrub(buf.as_mut_ptr(), 5), ER_ERR_NULL); // 非 4 倍数
    }
}

#[test]
fn null_pointers_are_checked() {
    unsafe {
        assert_eq!(er_frame_size(std::ptr::null_mut()), ER_ERR_NULL);
        assert_eq!(er_set_privacy(std::ptr::null_mut(), 1), ER_ERR_NULL);
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
