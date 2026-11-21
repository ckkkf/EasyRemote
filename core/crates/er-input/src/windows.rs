//! Windows 真实输入注入（SendInput）。
//!
//! 与 macOS 的差异：Windows 的键盘事件用**虚拟键码**（VK_*），
//! 而我们的协议传的是 USB HID 扫描码，需要先映射；
//! 发送时再通过 KEYEVENTF_SCANCODE 让系统按物理键处理，
//! 这样不受键盘布局影响。

#![cfg(target_os = "windows")]

use std::mem::size_of;

use crate::{InjectError, Injector, InputEvent, MouseButton};

type Bool = i32;
type Uint = u32;
type Word = u16;
type Dword = u32;
type Long = i32;

const INPUT_MOUSE: Dword = 0;
const INPUT_KEYBOARD: Dword = 1;

const KEYEVENTF_EXTENDEDKEY: Dword = 0x0001;
const KEYEVENTF_KEYUP: Dword = 0x0002;
const KEYEVENTF_SCANCODE: Dword = 0x0008;

const MOUSEEVENTF_MOVE: Dword = 0x0001;
const MOUSEEVENTF_LEFTDOWN: Dword = 0x0002;
const MOUSEEVENTF_LEFTUP: Dword = 0x0004;
const MOUSEEVENTF_RIGHTDOWN: Dword = 0x0008;
const MOUSEEVENTF_RIGHTUP: Dword = 0x0010;
const MOUSEEVENTF_MIDDLEDOWN: Dword = 0x0020;
const MOUSEEVENTF_MIDDLEUP: Dword = 0x0040;
const MOUSEEVENTF_XDOWN: Dword = 0x0080;
const MOUSEEVENTF_XUP: Dword = 0x0100;
const MOUSEEVENTF_WHEEL: Dword = 0x0800;
const MOUSEEVENTF_ABSOLUTE: Dword = 0x8000;
const XBUTTON1: Dword = 0x0001;
const XBUTTON2: Dword = 0x0002;
const WHEEL_DELTA: Long = 120;

#[repr(C)]
#[derive(Clone, Copy)]
struct MOUSEINPUT {
    dx: Long,
    dy: Long,
    mouse_data: Dword,
    dw_flags: Dword,
    time: Dword,
    dw_extra_info: usize,
}

#[repr(C)]
#[derive(Clone, Copy)]
struct KEYBDINPUT {
    w_vk: Word,
    w_scan: Word,
    dw_flags: Dword,
    time: Dword,
    dw_extra_info: usize,
}

#[repr(C)]
#[derive(Clone, Copy)]
struct HARDWAREINPUT {
    u_msg: Dword,
    w_param_l: Word,
    w_param_h: Word,
}

#[repr(C)]
#[derive(Clone, Copy)]
union INPUT_UNION {
    mi: MOUSEINPUT,
    ki: KEYBDINPUT,
    hi: HARDWAREINPUT,
}

#[repr(C)]
#[derive(Clone, Copy)]
struct INPUT {
    type_: Dword,
    u: INPUT_UNION,
}

#[repr(C)]
struct POINT {
    x: Long,
    y: Long,
}

#[link(name = "user32")]
extern "system" {
    fn SendInput(count: Uint, inputs: *const INPUT, size: i32) -> Uint;
    fn GetCursorPos(point: *mut POINT) -> Bool;
    fn SetCursorPos(x: i32, y: i32) -> Bool;
    fn GetSystemMetrics(index: i32) -> i32;
    fn SetProcessDpiAwarenessContext(value: isize) -> Bool;
}

const SM_CXSCREEN: i32 = 0;
const SM_CYSCREEN: i32 = 1;
/// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
const DPI_AWARENESS_PER_MONITOR_V2: isize = -4;

/// USB HID 扫描码 → Windows 虚拟键码（VK_*）。
///
/// 未收录的键返回 None（上层丢弃并计数）。
pub fn hid_to_vk(hid: u16) -> Option<u16> {
    Some(match hid {
        // 字母 A-Z 的 VK 就是 ASCII 大写
        0x04 => 0x41, // A
        0x05 => 0x42,
        0x06 => 0x43,
        0x07 => 0x44,
        0x08 => 0x45,
        0x09 => 0x46,
        0x0A => 0x47,
        0x0B => 0x48,
        0x0C => 0x49,
        0x0D => 0x4A,
        0x0E => 0x4B,
        0x0F => 0x4C,
        0x10 => 0x4D,
        0x11 => 0x4E,
        0x12 => 0x4F,
        0x13 => 0x50,
        0x14 => 0x51,
        0x15 => 0x52,
        0x16 => 0x53,
        0x17 => 0x54,
        0x18 => 0x55,
        0x19 => 0x56,
        0x1A => 0x57,
        0x1B => 0x58,
        0x1C => 0x59,
        0x1D => 0x5A,
        // 数字 0-9
        0x1E => 0x31,
        0x1F => 0x32,
        0x20 => 0x33,
        0x21 => 0x34,
        0x22 => 0x35,
        0x23 => 0x36,
        0x24 => 0x37,
        0x25 => 0x38,
        0x26 => 0x39,
        0x27 => 0x30,
        // 控制键
        0x28 => 0x0D, // VK_RETURN
        0x29 => 0x1B, // VK_ESCAPE
        0x2A => 0x08, // VK_BACK
        0x2B => 0x09, // VK_TAB
        0x2C => 0x20, // VK_SPACE
        0x2D => 0xBD, // VK_OEM_MINUS
        0x2E => 0xBB, // VK_OEM_PLUS
        0x2F => 0xDB, // VK_OEM_4
        0x30 => 0xDD, // VK_OEM_6
        0x31 => 0xDC, // VK_OEM_5
        0x33 => 0xBA, // VK_OEM_1
        0x34 => 0xDE, // VK_OEM_7
        0x35 => 0xC0, // VK_OEM_3
        0x36 => 0xBC, // VK_OEM_COMMA
        0x37 => 0xBE, // VK_OEM_PERIOD
        0x38 => 0xBF, // VK_OEM_2
        // 功能键
        0x3A => 0x70,
        0x3B => 0x71,
        0x3C => 0x72,
        0x3D => 0x73,
        0x3E => 0x74,
        0x3F => 0x75,
        0x40 => 0x76,
        0x41 => 0x77,
        0x42 => 0x78,
        0x43 => 0x79,
        0x44 => 0x7A,
        0x45 => 0x7B,
        // 方向键
        0x4F => 0x27, // RIGHT
        0x50 => 0x25, // LEFT
        0x51 => 0x28, // DOWN
        0x52 => 0x26, // UP
        // 修饰键
        0xE0 | 0xE4 => 0x11, // VK_CONTROL
        0xE1 | 0xE5 => 0x10, // VK_SHIFT
        0xE2 | 0xE6 => 0x12, // VK_MENU
        0xE3 | 0xE7 => 0x5B, // VK_LWIN
        _ => return None,
    })
}

fn key_input(vk: Word, scan: Word, up: bool, extended: bool) -> INPUT {
    let mut flags = KEYEVENTF_SCANCODE;
    if up {
        flags |= KEYEVENTF_KEYUP;
    }
    if extended {
        flags |= KEYEVENTF_EXTENDEDKEY;
    }
    INPUT {
        type_: INPUT_KEYBOARD,
        u: INPUT_UNION {
            ki: KEYBDINPUT { w_vk: vk, w_scan: scan, dw_flags: flags, time: 0, dw_extra_info: 0 },
        },
    }
}

fn mouse_input(dx: Long, dy: Long, data: Dword, flags: Dword) -> INPUT {
    INPUT {
        type_: INPUT_MOUSE,
        u: INPUT_UNION {
            mi: MOUSEINPUT { dx, dy, mouse_data: data, dw_flags: flags, time: 0, dw_extra_info: 0 },
        },
    }
}

fn send_one(input: INPUT) -> Result<(), InjectError> {
    let sent = unsafe { SendInput(1, &input, size_of::<INPUT>() as i32) };
    if sent == 1 {
        Ok(())
    } else {
        Err(InjectError::PermissionDenied)
    }
}

/// 进程级 DPI 感知初始化。
///
/// 必须在**创建任何窗口/HWND 之前**调用，否则 SetProcessDpiAwarenessContext 会失败，
/// 进程停留在 DPI 虚拟化状态：GetSystemMetrics 返回物理分辨率（如 2880x1620），
/// 而 SetCursorPos 却按逻辑坐标（1646x926）解释，落点必然偏移。
pub fn init_process_dpi() -> bool {
    unsafe { SetProcessDpiAwarenessContext(DPI_AWARENESS_PER_MONITOR_V2) != 0 }
}

/// Windows 注入器。
pub struct WindowsInjector {
    live: bool,
    /// 是否已申请 DPI 感知（绝对坐标需要）
    dpi_ready: bool,
}

impl WindowsInjector {
    pub fn new(live: bool) -> Self {
        // DPI 感知必须在**任何** GetCursorPos / SetCursorPos 之前设好：
        // 150% 缩放下未感知进程拿到的是逻辑坐标（1920x1080），
        // 注入用物理坐标（2880x1620），回读时就必然出现成比例的偏移。
        unsafe {
            let _ = SetProcessDpiAwarenessContext(DPI_AWARENESS_PER_MONITOR_V2);
        }
        Self { live, dpi_ready: true }
    }

    /// 读取当前光标位置（自检用）。
    pub fn cursor_position() -> Option<(i32, i32)> {
        let mut p = POINT { x: 0, y: 0 };
        let ok = unsafe { GetCursorPos(&mut p) };
        if ok == 0 {
            None
        } else {
            Some((p.x, p.y))
        }
    }

    /// 屏幕尺寸（用于把像素坐标转成 0..65535 的绝对坐标）。
    pub fn screen_size() -> (i32, i32) {
        unsafe { (GetSystemMetrics(SM_CXSCREEN), GetSystemMetrics(SM_CYSCREEN)) }
    }

    fn ensure_dpi_aware(&mut self) -> bool {
        if self.dpi_ready {
            return true;
        }
        self.dpi_ready = init_process_dpi();
        self.dpi_ready
    }
}

impl Injector for WindowsInjector {
    fn is_live(&self) -> bool {
        self.live
    }

    fn inject(&mut self, event: &InputEvent) -> Result<(), InjectError> {
        match event {
            InputEvent::Key { scancode, pressed } => {
                let vk = hid_to_vk(*scancode).ok_or(InjectError::UnknownKey)?;
                if !self.live {
                    return Ok(());
                }
                send_one(key_input(vk, *scancode, !*pressed, false))
            }
            InputEvent::MouseMove { x, y } => {
                if !self.live {
                    return Ok(());
                }
                if !self.ensure_dpi_aware() {
                    // 没有 per-monitor DPI 感知时坐标系不可信：宁可报错，
                    // 也不要把物理坐标当逻辑坐标用（会偏数十像素，看起来像"注入不准"）。
                    return Err(InjectError::Unsupported);
                }
                // 用 SetCursorPos 而不是 SendInput 的绝对坐标：
                // SendInput 的 MOUSEEVENTF_ABSOLUTE 要先归一化到 0..65535，
                // 在 2880x1620 下每单位约 0.044px，取整会带来 ±2px 的系统性偏差
                // （实测目标 400,300 落到 404,298）。SetCursorPos 是精确像素。
                let ok = unsafe { SetCursorPos(*x, *y) };
                if ok == 0 {
                    return Err(InjectError::OutOfBounds);
                }
                Ok(())
            }
            InputEvent::MouseButton { button, pressed } => {
                if !self.live {
                    return Ok(());
                }
                let (flags, data) = match (button, pressed) {
                    (MouseButton::Left, true) => (MOUSEEVENTF_LEFTDOWN, 0),
                    (MouseButton::Left, false) => (MOUSEEVENTF_LEFTUP, 0),
                    (MouseButton::Right, true) => (MOUSEEVENTF_RIGHTDOWN, 0),
                    (MouseButton::Right, false) => (MOUSEEVENTF_RIGHTUP, 0),
                    (MouseButton::Middle, true) => (MOUSEEVENTF_MIDDLEDOWN, 0),
                    (MouseButton::Middle, false) => (MOUSEEVENTF_MIDDLEUP, 0),
                    (MouseButton::X1, true) => (MOUSEEVENTF_XDOWN, XBUTTON1),
                    (MouseButton::X1, false) => (MOUSEEVENTF_XUP, XBUTTON1),
                    (MouseButton::X2, true) => (MOUSEEVENTF_XDOWN, XBUTTON2),
                    (MouseButton::X2, false) => (MOUSEEVENTF_XUP, XBUTTON2),
                };
                send_one(mouse_input(0, 0, data, flags))
            }
            InputEvent::Wheel { dx, dy } => {
                if !self.live || *dy == 0 {
                    return Ok(());
                }
                let _ = dx;
                send_one(mouse_input(0, 0, (*dy * WHEEL_DELTA) as Dword, MOUSEEVENTF_WHEEL))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::scancode_for;

    #[test]
    fn letters_map_to_distinct_vk() {
        let mut seen = std::collections::HashSet::new();
        for c in 'a'..='z' {
            let hid = scancode_for(&c.to_string()).expect("hid");
            let vk = hid_to_vk(hid).expect("vk");
            assert!(seen.insert(vk), "duplicate VK for {c}");
        }
    }

    #[test]
    fn known_keys_map_correctly() {
        assert_eq!(hid_to_vk(scancode_for("a").unwrap()), Some(0x41));
        assert_eq!(hid_to_vk(scancode_for("return").unwrap()), Some(0x0D));
        assert_eq!(hid_to_vk(scancode_for("space").unwrap()), Some(0x20));
        assert_eq!(hid_to_vk(scancode_for("esc").unwrap()), Some(0x1B));
    }

    #[test]
    fn unknown_hid_rejected() {
        assert_eq!(hid_to_vk(0xFFFF), None);
    }

    #[test]
    fn observer_mode_does_not_post() {
        let mut inj = WindowsInjector::new(false);
        assert!(!inj.is_live());
        assert!(inj.inject(&InputEvent::MouseMove { x: 1, y: 1 }).is_ok());
    }
}
