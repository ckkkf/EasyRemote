//! Windows 注入自检程序：独立可编译，只依赖 std + user32。
//!
//! 用法：cargo run --release
//! 输出一行 JSON 结果，便于另一端的 agent 解析。

use std::mem::size_of;

type Bool = i32;
type Uint = u32;
type Word = u16;
type Dword = u32;
type Long = i32;

const INPUT_MOUSE: Dword = 0;
const INPUT_KEYBOARD: Dword = 1;
const KEYEVENTF_KEYUP: Dword = 0x0002;
const KEYEVENTF_SCANCODE: Dword = 0x0008;
const MOUSEEVENTF_MOVE: Dword = 0x0001;
const MOUSEEVENTF_ABSOLUTE: Dword = 0x8000;

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
    fn GetSystemMetrics(index: i32) -> i32;
    fn SetProcessDpiAwarenessContext(value: isize) -> Bool;
}

const SM_CXSCREEN: i32 = 0;
const SM_CYSCREEN: i32 = 1;
const DPI_AWARENESS_PER_MONITOR_V2: isize = -4;

fn cursor() -> Option<(i32, i32)> {
    let mut p = POINT { x: 0, y: 0 };
    let ok = unsafe { GetCursorPos(&mut p) };
    if ok == 0 { None } else { Some((p.x, p.y)) }
}

fn screen() -> (i32, i32) {
    unsafe { (GetSystemMetrics(SM_CXSCREEN), GetSystemMetrics(SM_CYSCREEN)) }
}

fn move_abs(x: i32, y: i32) -> u32 {
    let (w, h) = screen();
    let nx = ((x as i64 * 65535) / w as i64).clamp(0, 65535) as i32;
    let ny = ((y as i64 * 65535) / h as i64).clamp(0, 65535) as i32;
    let input = INPUT {
        type_: INPUT_MOUSE,
        u: INPUT_UNION {
            mi: MOUSEINPUT {
                dx: nx,
                dy: ny,
                mouse_data: 0,
                dw_flags: MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE,
                time: 0,
                dw_extra_info: 0,
            },
        },
    };
    unsafe { SendInput(1, &input, size_of::<INPUT>() as i32) }
}

/// 发一个 Shift 按下/抬起（无害，不会输入字符）来验证键盘通道。
fn tap_shift() -> (u32, u32) {
    let down = INPUT {
        type_: INPUT_KEYBOARD,
        u: INPUT_UNION {
            ki: KEYBDINPUT {
                w_vk: 0x10,
                w_scan: 0xE1,
                dw_flags: KEYEVENTF_SCANCODE,
                time: 0,
                dw_extra_info: 0,
            },
        },
    };
    let up = INPUT {
        type_: INPUT_KEYBOARD,
        u: INPUT_UNION {
            ki: KEYBDINPUT {
                w_vk: 0x10,
                w_scan: 0xE1,
                dw_flags: KEYEVENTF_SCANCODE | KEYEVENTF_KEYUP,
                time: 0,
                dw_extra_info: 0,
            },
        },
    };
    unsafe {
        (
            SendInput(1, &down, size_of::<INPUT>() as i32),
            SendInput(1, &up, size_of::<INPUT>() as i32),
        )
    }
}

fn main() {
    unsafe {
        let _ = SetProcessDpiAwarenessContext(DPI_AWARENESS_PER_MONITOR_V2);
    }

    let (sw, sh) = screen();
    let before = cursor();
    let sent = move_abs(300, 300);
    std::thread::sleep(std::time::Duration::from_millis(150));
    let after = cursor();
    let (kdown, kup) = tap_shift();

    let moved = match (before, after) {
        (Some(_), Some((x, y))) => (x - 300).abs() <= 8 && (y - 300).abs() <= 8,
        _ => false,
    };

    println!(
        "{{\"screen\":\"{}x{}\",\"cursor_before\":{:?},\"cursor_after\":{:?},\"sendinput_move\":{},\"key_down\":{},\"key_up\":{},\"moved_to_target\":{}}}",
        sw, sh, before, after, sent, kdown, kup, moved
    );

    if sent == 1 && moved {
        println!("RESULT: PASS");
    } else {
        println!("RESULT: FAIL");
        std::process::exit(1);
    }
}
