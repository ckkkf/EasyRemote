//! 输入事件的线格式：紧凑二进制，跨端一致。
//!
//! 布局：kind(u8) + 载荷
//!   1 Key:      scancode(u16 BE) + pressed(u8)
//!   2 MouseMove: x(i32 BE) + y(i32 BE)
//!   3 MouseBtn: button(u8) + pressed(u8)
//!   4 Wheel:    dx(i32 BE) + dy(i32 BE)

use crate::{InputEvent, MouseButton};

/// 线格式标签。
pub const TAG_KEY: u8 = 1;
pub const TAG_MOVE: u8 = 2;
pub const TAG_BUTTON: u8 = 3;
pub const TAG_WHEEL: u8 = 4;

impl InputEvent {
    /// 编码成线格式。
    pub fn encode(&self) -> Vec<u8> {
        match self {
            InputEvent::Key { scancode, pressed } => {
                let mut v = vec![TAG_KEY];
                v.extend_from_slice(&scancode.to_be_bytes());
                v.push(u8::from(*pressed));
                v
            }
            InputEvent::MouseMove { x, y } => {
                let mut v = vec![TAG_MOVE];
                v.extend_from_slice(&x.to_be_bytes());
                v.extend_from_slice(&y.to_be_bytes());
                v
            }
            InputEvent::MouseButton { button, pressed } => {
                vec![TAG_BUTTON, encode_button(*button), u8::from(*pressed)]
            }
            InputEvent::Wheel { dx, dy } => {
                let mut v = vec![TAG_WHEEL];
                v.extend_from_slice(&dx.to_be_bytes());
                v.extend_from_slice(&dy.to_be_bytes());
                v
            }
        }
    }

    /// 从线格式解码；格式不对返回 None（上层丢弃并计数）。
    pub fn decode(buf: &[u8]) -> Option<InputEvent> {
        match *buf.first()? {
            TAG_KEY => {
                if buf.len() < 4 {
                    return None;
                }
                let scancode = u16::from_be_bytes([buf[1], buf[2]]);
                Some(InputEvent::Key { scancode, pressed: buf[3] != 0 })
            }
            TAG_MOVE => {
                if buf.len() < 9 {
                    return None;
                }
                let x = i32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);
                let y = i32::from_be_bytes([buf[5], buf[6], buf[7], buf[8]]);
                Some(InputEvent::MouseMove { x, y })
            }
            TAG_BUTTON => {
                if buf.len() < 3 {
                    return None;
                }
                Some(InputEvent::MouseButton {
                    button: decode_button(buf[1])?,
                    pressed: buf[2] != 0,
                })
            }
            TAG_WHEEL => {
                if buf.len() < 9 {
                    return None;
                }
                let dx = i32::from_be_bytes([buf[1], buf[2], buf[3], buf[4]]);
                let dy = i32::from_be_bytes([buf[5], buf[6], buf[7], buf[8]]);
                Some(InputEvent::Wheel { dx, dy })
            }
            _ => None,
        }
    }
}

fn encode_button(b: MouseButton) -> u8 {
    match b {
        MouseButton::Left => 1,
        MouseButton::Right => 2,
        MouseButton::Middle => 3,
        MouseButton::X1 => 4,
        MouseButton::X2 => 5,
    }
}

fn decode_button(v: u8) -> Option<MouseButton> {
    Some(match v {
        1 => MouseButton::Left,
        2 => MouseButton::Right,
        3 => MouseButton::Middle,
        4 => MouseButton::X1,
        5 => MouseButton::X2,
        _ => return None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn roundtrip(e: InputEvent) {
        let enc = e.encode();
        assert_eq!(InputEvent::decode(&enc), Some(e));
    }

    #[test]
    fn key_roundtrip() {
        roundtrip(InputEvent::Key { scancode: 0x04, pressed: true });
        roundtrip(InputEvent::Key { scancode: 0xE1, pressed: false });
    }

    #[test]
    fn mouse_roundtrip() {
        roundtrip(InputEvent::MouseMove { x: 1920, y: 1080 });
        roundtrip(InputEvent::MouseMove { x: -1, y: 0 });
        roundtrip(InputEvent::MouseButton { button: MouseButton::Right, pressed: true });
        roundtrip(InputEvent::Wheel { dx: 0, dy: -3 });
    }

    #[test]
    fn truncated_rejected() {
        assert_eq!(InputEvent::decode(&[]), None);
        assert_eq!(InputEvent::decode(&[TAG_KEY, 0]), None);
        assert_eq!(InputEvent::decode(&[TAG_MOVE, 0, 0]), None);
        assert_eq!(InputEvent::decode(&[TAG_BUTTON, 1]), None);
    }

    #[test]
    fn unknown_tag_rejected() {
        assert_eq!(InputEvent::decode(&[99, 1, 2, 3]), None);
    }

    #[test]
    fn unknown_button_rejected() {
        assert_eq!(InputEvent::decode(&[TAG_BUTTON, 99, 1]), None);
    }
}
