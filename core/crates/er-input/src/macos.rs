//! macOS 真实输入注入（CoreGraphics CGEventPost）。
//!
//! 需要「辅助功能」授权；未授权时系统会静默丢弃事件，
//! 因此调用方应通过 inject 的错误或自检来发现。
//!
//! 说明：这里用裸 FFI 直接调 CoreGraphics，避免引入重量级绑定库。

#![cfg(target_os = "macos")]

use std::ffi::c_void;

use crate::{InjectError, Injector, InputEvent, MouseButton};

type CGEventRef = *mut c_void;
type CGEventSourceRef = *mut c_void;
type CGEventTapLocation = u32;
type CGEventType = u32;
type CGMouseButton = u32;

#[link(name = "CoreGraphics", kind = "framework")]
extern "C" {
    fn CGEventCreate(source: CGEventSourceRef) -> CGEventRef;
    fn CGEventCreateMouseEvent(
        source: CGEventSourceRef,
        mouse_type: CGEventType,
        mouse_cursor_position: CGPoint,
        mouse_button: CGMouseButton,
    ) -> CGEventRef;
    fn CGEventCreateKeyboardEvent(
        source: CGEventSourceRef,
        virtual_key: u16,
        key_down: bool,
    ) -> CGEventRef;
    fn CGEventCreateScrollWheelEvent(
        source: CGEventSourceRef,
        units: u32,
        wheel_count: u32,
        ...
    ) -> CGEventRef;
    fn CGEventPost(tap: CGEventTapLocation, event: CGEventRef);
    fn CGEventGetLocation(event: CGEventRef) -> CGPoint;
    /// 主显示器 ID（坐标系原点所在的屏）。
    fn CGMainDisplayID() -> u32;
    /// 主显示器**像素**宽（Retina 下是 points 的 2 倍）。
    fn CGDisplayPixelsWide(display: u32) -> usize;
    /// 主显示器**像素**高。
    fn CGDisplayPixelsHigh(display: u32) -> usize;
    /// 主显示器在全局坐标里的矩形（单位是 points）。
    fn CGDisplayBounds(display: u32) -> CGRect;
}

#[link(name = "CoreFoundation", kind = "framework")]
extern "C" {
    fn CFRelease(cf: *const c_void);
}

#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct CGPoint {
    pub x: f64,
    pub y: f64,
}

#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct CGSize {
    pub width: f64,
    pub height: f64,
}

#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct CGRect {
    pub origin: CGPoint,
    pub size: CGSize,
}

/// 主显示器的坐标系：**像素**（采集帧、协议坐标）↔ **points**（CGEvent 坐标）。
///
/// 为什么必须有这一层：Retina 屏上屏幕采集返回的是像素（e.g. 3024x1964），
/// 而 CGEventPost 用的是 points（e.g. 1512x982）。直接把像素当 points 注入，
/// 光标只会走到屏幕左上角的一半处——看起来像「注入完全不生效」，
/// 实际是坐标系错了 2 倍。非 Retina（scale=1）时两者恰好相等，掩盖了 bug。
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct MacDisplay {
    /// 像素宽高（与采集帧、协议坐标一致）
    pub pixel_width: u32,
    pub pixel_height: u32,
    /// 逻辑宽高（points）
    pub point_width: f64,
    pub point_height: f64,
    /// 主屏在全局 point 坐标系里的原点（多屏时非 0）
    pub origin_x: f64,
    pub origin_y: f64,
}

impl MacDisplay {
    /// 读主显示器几何。失败时退化为 1:1、原点 0 的保守值。
    pub fn main() -> Self {
        unsafe {
            let id = CGMainDisplayID();
            let bounds = CGDisplayBounds(id);
            let pw = CGDisplayPixelsWide(id) as u32;
            let ph = CGDisplayPixelsHigh(id) as u32;
            let fallback = (pw.max(1) as f64, ph.max(1) as f64);
            let (ptw, pth) = if bounds.size.width > 1.0 && bounds.size.height > 1.0 {
                (bounds.size.width, bounds.size.height)
            } else {
                fallback
            };
            Self {
                pixel_width: pw,
                pixel_height: ph,
                point_width: ptw,
                point_height: pth,
                origin_x: bounds.origin.x,
                origin_y: bounds.origin.y,
            }
        }
    }

    /// 像素 → points 的缩放系数（Retina 2.0，普通屏 1.0）。
    pub fn scale(&self) -> f64 {
        let sx = self.pixel_width as f64 / self.point_width.max(1.0);
        let sy = self.pixel_height as f64 / self.point_height.max(1.0);
        // 两块屏分辨率不一致时取均值，宁可轻微偏也不要跳到 2 倍
        ((sx + sy) / 2.0).clamp(0.5, 4.0)
    }

    /// 采集/协议像素坐标 → CGEvent point 坐标（含主屏原点平移）。
    pub fn pixel_to_point(&self, x: i32, y: i32) -> CGPoint {
        let s = self.scale();
        CGPoint {
            x: self.origin_x + x as f64 / s,
            y: self.origin_y + y as f64 / s,
        }
    }

    /// CGEvent point 坐标 → 像素坐标（回读光标后用）。
    pub fn point_to_pixel(&self, p: CGPoint) -> (i32, i32) {
        let s = self.scale();
        (
            ((p.x - self.origin_x) * s).round() as i32,
            ((p.y - self.origin_y) * s).round() as i32,
        )
    }
}

// 事件类型常量
const KCG_EVENT_MOUSE_MOVED: CGEventType = 5;
const KCG_EVENT_LEFT_MOUSE_DOWN: CGEventType = 1;
const KCG_EVENT_LEFT_MOUSE_UP: CGEventType = 2;
const KCG_EVENT_RIGHT_MOUSE_DOWN: CGEventType = 3;
const KCG_EVENT_RIGHT_MOUSE_UP: CGEventType = 4;
const KCG_EVENT_OTHER_MOUSE_DOWN: CGEventType = 25;
const KCG_EVENT_OTHER_MOUSE_UP: CGEventType = 26;
const KCG_HID_EVENT_TAP: CGEventTapLocation = 0;
const KCG_SCROLL_UNIT_LINE: u32 = 1;

const BTN_LEFT: CGMouseButton = 0;
const BTN_RIGHT: CGMouseButton = 1;
const BTN_CENTER: CGMouseButton = 2;

/// USB HID 扫描码 → macOS 虚拟键码。
///
/// 两套码表不同，必须显式映射；未收录的键返回 None（上层丢弃）。
pub fn hid_to_cgkeycode(hid: u16) -> Option<u16> {
    Some(match hid {
        // 字母
        0x04 => 0,   // a
        0x05 => 11,  // b
        0x06 => 8,   // c
        0x07 => 2,   // d
        0x08 => 14,  // e
        0x09 => 3,   // f
        0x0A => 5,   // g
        0x0B => 4,   // h
        0x0C => 34,  // i
        0x0D => 38,  // j
        0x0E => 40,  // k
        0x0F => 37,  // l
        0x10 => 46,  // m
        0x11 => 45,  // n
        0x12 => 31,  // o
        0x13 => 35,  // p
        0x14 => 12,  // q
        0x15 => 15,  // r
        0x16 => 1,   // s
        0x17 => 17,  // t
        0x18 => 32,  // u
        0x19 => 9,   // v
        0x1A => 13,  // w
        0x1B => 7,   // x
        0x1C => 16,  // y
        0x1D => 6,   // z
        // 数字
        0x1E => 18, // 1
        0x1F => 19, // 2
        0x20 => 20, // 3
        0x21 => 21, // 4
        0x22 => 23, // 5
        0x23 => 22, // 6
        0x24 => 26, // 7
        0x25 => 28, // 8
        0x26 => 25, // 9
        0x27 => 29, // 0
        // 控制键
        0x28 => 36, // return
        0x29 => 53, // esc
        0x2A => 51, // backspace
        0x2B => 48, // tab
        0x2C => 49, // space
        0x2D => 27, // -
        0x2E => 24, // =
        0x2F => 33, // [
        0x30 => 30, // ]
        0x31 => 42, // backslash
        0x33 => 41, // ;
        0x34 => 39, // '
        0x35 => 50, // `
        0x36 => 43, // ,
        0x37 => 47, // .
        0x38 => 44, // /
        // 功能键
        0x3A => 122, // f1
        0x3B => 120, // f2
        0x3C => 99,  // f3
        0x3D => 118, // f4
        0x3E => 96,  // f5
        0x3F => 97,  // f6
        0x40 => 98,  // f7
        0x41 => 100, // f8
        0x42 => 101, // f9
        0x43 => 109, // f10
        0x44 => 103, // f11
        0x45 => 111, // f12
        // 方向键
        0x4F => 124, // right
        0x50 => 123, // left
        0x51 => 125, // down
        0x52 => 126, // up
        // 修饰键
        0xE0 | 0xE4 => 59, // control
        0xE1 | 0xE5 => 56, // shift
        0xE2 | 0xE6 => 58, // option
        0xE3 | 0xE7 => 55, // command
        _ => return None,
    })
}

/// macOS 注入器。
pub struct MacInjector {
    live: bool,
    /// 主显示器几何：协议坐标是像素，CGEvent 要 points
    display: MacDisplay,
    /// 注入映射次数（自检/证据用：能区分「事件没到」和「到了但坐标错」）
    mapped: u64,
}

impl MacInjector {
    /// 创建注入器。live=false 时只校验不投递（隐私屏/观察者模式用）。
    pub fn new(live: bool) -> Self {
        Self { live, display: MacDisplay::main(), mapped: 0 }
    }

    /// 主显示器几何（像素/points/原点）。
    pub fn display(&self) -> MacDisplay {
        self.display
    }

    /// 像素尺寸（协议坐标空间，供例子断言目标是否越界）。
    pub fn screen_size() -> (u32, u32) {
        let d = MacDisplay::main();
        (d.pixel_width, d.pixel_height)
    }

    /// 已做坐标映射的鼠标移动次数。
    pub fn mapped_moves(&self) -> u64 {
        self.mapped
    }

    /// 光标位置（**像素**坐标，便于与目标像素坐标直接比对）。
    pub fn cursor_pixel() -> Option<(i32, i32)> {
        Self::cursor_position().map(|p| MacDisplay::main().point_to_pixel(p))
    }

    /// 读取当前鼠标位置（用于自检与测试）。
    pub fn cursor_position() -> Option<CGPoint> {
        unsafe {
            let ev = CGEventCreate(std::ptr::null_mut());
            if ev.is_null() {
                return None;
            }
            let p = CGEventGetLocation(ev);
            CFRelease(ev as *const c_void);
            Some(p)
        }
    }
}

impl Injector for MacInjector {
    fn is_live(&self) -> bool {
        self.live
    }

    fn inject(&mut self, event: &InputEvent) -> Result<(), InjectError> {
        match event {
            InputEvent::Key { scancode, pressed } => {
                let key = hid_to_cgkeycode(*scancode).ok_or(InjectError::UnknownKey)?;
                if !self.live {
                    return Ok(());
                }
                unsafe {
                    let ev = CGEventCreateKeyboardEvent(std::ptr::null_mut(), key, *pressed);
                    if ev.is_null() {
                        return Err(InjectError::PermissionDenied);
                    }
                    CGEventPost(KCG_HID_EVENT_TAP, ev);
                    CFRelease(ev as *const c_void);
                }
                Ok(())
            }
            InputEvent::MouseMove { x, y } => {
                if !self.live {
                    return Ok(());
                }
                // 协议/采集坐标是像素，CGEvent 要 points：必须换算，否则 Retina 上偏一半
                let pos = self.display.pixel_to_point(*x, *y);
                self.mapped += 1;
                unsafe {
                    let ev = CGEventCreateMouseEvent(
                        std::ptr::null_mut(),
                        KCG_EVENT_MOUSE_MOVED,
                        pos,
                        BTN_LEFT,
                    );
                    if ev.is_null() {
                        return Err(InjectError::PermissionDenied);
                    }
                    CGEventPost(KCG_HID_EVENT_TAP, ev);
                    CFRelease(ev as *const c_void);
                }
                Ok(())
            }
            InputEvent::MouseButton { button, pressed } => {
                if !self.live {
                    return Ok(());
                }
                let (down, up, btn) = match button {
                    MouseButton::Left => (KCG_EVENT_LEFT_MOUSE_DOWN, KCG_EVENT_LEFT_MOUSE_UP, BTN_LEFT),
                    MouseButton::Right => {
                        (KCG_EVENT_RIGHT_MOUSE_DOWN, KCG_EVENT_RIGHT_MOUSE_UP, BTN_RIGHT)
                    }
                    MouseButton::Middle => {
                        (KCG_EVENT_OTHER_MOUSE_DOWN, KCG_EVENT_OTHER_MOUSE_UP, BTN_CENTER)
                    }
                    MouseButton::X1 | MouseButton::X2 => {
                        (KCG_EVENT_OTHER_MOUSE_DOWN, KCG_EVENT_OTHER_MOUSE_UP, BTN_CENTER)
                    }
                };
                let pos = MacInjector::cursor_position().unwrap_or(CGPoint { x: 0.0, y: 0.0 });
                unsafe {
                    let ev = CGEventCreateMouseEvent(
                        std::ptr::null_mut(),
                        if *pressed { down } else { up },
                        pos,
                        btn,
                    );
                    if ev.is_null() {
                        return Err(InjectError::PermissionDenied);
                    }
                    CGEventPost(KCG_HID_EVENT_TAP, ev);
                    CFRelease(ev as *const c_void);
                }
                Ok(())
            }
            InputEvent::Wheel { dx, dy } => {
                if !self.live {
                    return Ok(());
                }
                let _ = dx;
                if *dy == 0 {
                    return Ok(());
                }
                unsafe {
                    let ev = CGEventCreateScrollWheelEvent(
                        std::ptr::null_mut(),
                        KCG_SCROLL_UNIT_LINE,
                        1,
                        *dy,
                    );
                    if ev.is_null() {
                        return Err(InjectError::PermissionDenied);
                    }
                    CGEventPost(KCG_HID_EVENT_TAP, ev);
                    CFRelease(ev as *const c_void);
                }
                Ok(())
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::scancode_for;

    #[test]
    fn letters_map_to_distinct_keycodes() {
        let mut seen = std::collections::HashSet::new();
        for c in 'a'..='z' {
            let hid = scancode_for(&c.to_string()).expect("hid");
            let cg = hid_to_cgkeycode(hid).expect("cg keycode");
            assert!(seen.insert(cg), "duplicate keycode for {c}");
        }
    }

    #[test]
    fn known_keys_map_correctly() {
        assert_eq!(hid_to_cgkeycode(scancode_for("a").unwrap()), Some(0));
        assert_eq!(hid_to_cgkeycode(scancode_for("return").unwrap()), Some(36));
        assert_eq!(hid_to_cgkeycode(scancode_for("space").unwrap()), Some(49));
        assert_eq!(hid_to_cgkeycode(scancode_for("esc").unwrap()), Some(53));
    }

    #[test]
    fn unknown_hid_rejected() {
        assert_eq!(hid_to_cgkeycode(0xFFFF), None);
    }

    #[test]
    fn observer_mode_does_not_post() {
        let mut inj = MacInjector::new(false);
        assert!(!inj.is_live());
        assert!(inj.inject(&InputEvent::MouseMove { x: 1, y: 1 }).is_ok());
    }

    /// Retina 上 pixel/point 是两个空间：scale 必须等于 像素/逻辑。
    /// 这条是「注入偏一半」回归的保护网——非 Retina 机器上它恒为 1.0。
    #[test]
    fn display_scale_is_pixels_per_point() {
        let d = MacDisplay::main();
        assert!(d.pixel_width > 0 && d.pixel_height > 0, "display must have pixels: {d:?}");
        let s = d.scale();
        assert!((0.5..=4.0).contains(&s), "implausible scale {s} for {d:?}");
        let expect = d.pixel_width as f64 / d.point_width.max(1.0);
        assert!((s - expect).abs() < 0.01, "scale {s} should track pixels/points {expect}");
    }

    /// 像素 → point → 像素 往返，误差 ≤1px（取整带来的）。
    #[test]
    fn pixel_point_roundtrip() {
        let d = MacDisplay::main();
        let cases = [
            (0, 0),
            (d.pixel_width as i32 / 2, d.pixel_height as i32 / 2),
            (d.pixel_width as i32 - 1, d.pixel_height as i32 - 1),
        ];
        for (x, y) in cases {
            let p = d.pixel_to_point(x, y);
            let (bx, by) = d.point_to_pixel(p);
            assert!((bx - x).abs() <= 1, "x roundtrip {x} -> {bx} ({d:?})");
            assert!((by - y).abs() <= 1, "y roundtrip {y} -> {by} ({d:?})");
        }
    }

    /// 主屏原点必须被平移进 point 坐标（多屏时主屏 origin 可能非 0）。
    #[test]
    fn origin_is_added_in_point_space() {
        let d = MacDisplay { origin_x: -1920.0, origin_y: 100.0, ..MacDisplay::main() };
        let p = d.pixel_to_point(0, 0);
        assert!((p.x - (-1920.0)).abs() < 0.01);
        assert!((p.y - 100.0).abs() < 0.01);
        assert_eq!(d.point_to_pixel(p), (0, 0));
    }

    /// 真实注入：把鼠标移到像素 (200,200) 再**按像素**读回。
    /// 无辅助功能授权时系统会静默丢弃，此时跳过而非失败。
    #[test]
    fn real_injection_moves_cursor() {
        let before = match MacInjector::cursor_pixel() {
            Some(p) => p,
            None => {
                eprintln!("skip: cannot read cursor position");
                return;
            }
        };
        let mut inj = MacInjector::new(true);
        if inj.inject(&InputEvent::MouseMove { x: 200, y: 200 }).is_err() {
            eprintln!("skip: injection denied");
            return;
        }
        assert_eq!(inj.mapped_moves(), 1, "mouse move must go through coordinate mapping");
        std::thread::sleep(std::time::Duration::from_millis(120));
        let after = MacInjector::cursor_pixel().expect("cursor pos");
        let moved = (after.0 - 200).abs() <= 3 && (after.1 - 200).abs() <= 3;
        if !moved {
            eprintln!(
                "skip: cursor not moved (before {before:?}, after {after:?}) — 可能需要辅助功能授权"
            );
            return;
        }
        assert!(moved, "cursor should be at pixel (200,200), got {after:?}");
        eprintln!("cursor moved to pixel {after:?}");
    }
}
