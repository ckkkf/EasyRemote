//! 输入注入（PRD §5.3）。
//!
//! 分三层：
//!   1. **事件模型**：与平台无关的 `InputEvent`（主控端产生）
//!   2. **映射**：字符串键名 → 物理扫描码（跨平台一致，避免受键盘布局影响）
//!   3. **注入**：`Injector` trait，平台实现负责真正发给系统
//!
//! 为什么用扫描码而不是字符：被控端可能是不同键盘布局，
//! 传字符会导致「我按 A 他那边出 Q」。扫描码是物理位置，语义稳定。

pub mod wire;

#[cfg(target_os = "windows")]
pub mod windows;

#[cfg(target_os = "macos")]
pub mod macos;

/// 鼠标按键。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MouseButton {
    Left,
    Right,
    Middle,
    X1,
    X2,
}

/// 与平台无关的输入事件。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InputEvent {
    /// 键盘按下/抬起，用扫描码
    Key { scancode: u16, pressed: bool },
    /// 鼠标移动（绝对坐标，归一到被控端分辨率）
    MouseMove { x: i32, y: i32 },
    /// 鼠标按键
    MouseButton { button: MouseButton, pressed: bool },
    /// 滚轮：正值向上/向右
    Wheel { dx: i32, dy: i32 },
}

/// 注入错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum InjectError {
    /// 平台拒绝（权限，如 macOS 辅助功能未授权）
    PermissionDenied,
    /// 未知扫描码
    UnknownKey,
    /// 坐标越界
    OutOfBounds,
    /// 平台能力缺失（例如进程没有 DPI 感知，坐标系不可信）
    Unsupported,
}

impl std::fmt::Display for InjectError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            InjectError::PermissionDenied => write!(f, "input injection permission denied"),
            InjectError::UnknownKey => write!(f, "unknown key"),
            InjectError::OutOfBounds => write!(f, "coordinate out of bounds"),
            InjectError::Unsupported => write!(f, "injection unsupported on this process"),
        }
    }
}

impl std::error::Error for InjectError {}

/// 注入器。
pub trait Injector {
    /// 是不是真的往系统里发（false = 只在本地记录，用于测试与隐私屏）。
    fn is_live(&self) -> bool;
    fn inject(&mut self, event: &InputEvent) -> Result<(), InjectError>;
}

/// 屏幕坐标系：把归一化坐标映射到目标分辨率。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ScreenGeom {
    pub width: u32,
    pub height: u32,
}

impl ScreenGeom {
    /// 归一化坐标（0..=65535）→ 像素坐标。
    pub fn denormalize(&self, nx: u16, ny: u16) -> (i32, i32) {
        let x = (nx as u64 * self.width as u64) / 65535;
        let y = (ny as u64 * self.height as u64) / 65535;
        (x as i32, y as i32)
    }

    /// 像素坐标 → 归一化坐标。
    pub fn normalize(&self, x: i32, y: i32) -> (u16, u16) {
        let nx = (x.max(0) as u64 * 65535) / self.width.max(1) as u64;
        let ny = (y.max(0) as u64 * 65535) / self.height.max(1) as u64;
        (nx.min(65535) as u16, ny.min(65535) as u16)
    }

    pub fn contains(&self, x: i32, y: i32) -> bool {
        x >= 0 && y >= 0 && (x as u32) < self.width && (y as u32) < self.height
    }
}

/// 记录型注入器：统计事件，用于测试与「隐私屏/只读观察」模式。
#[derive(Debug, Default)]
pub struct RecordingInjector {
    pub events: Vec<InputEvent>,
    pub live: bool,
    pub deny: bool,
}

impl RecordingInjector {
    pub fn new() -> Self {
        Self::default()
    }
    pub fn live() -> Self {
        Self {
            events: Vec::new(),
            live: true,
            deny: false,
        }
    }
    pub fn count(&self) -> usize {
        self.events.len()
    }
}

impl Injector for RecordingInjector {
    fn is_live(&self) -> bool {
        self.live
    }

    fn inject(&mut self, event: &InputEvent) -> Result<(), InjectError> {
        if self.deny {
            return Err(InjectError::PermissionDenied);
        }
        if !self.live {
            return Ok(()); // 观察者模式：不注入
        }
        self.events.push(event.clone());
        Ok(())
    }
}

/// 键名 → 扫描码（USB HID Usage ID，跨平台一致）。
///
/// 只收录常用键；未收录的键返回 `None`，由上层决定是否上报不支持。
pub fn scancode_for(name: &str) -> Option<u16> {
    let key = name.to_ascii_lowercase();
    let code = match key.as_str() {
        "a" => 0x04,
        "b" => 0x05,
        "c" => 0x06,
        "d" => 0x07,
        "e" => 0x08,
        "f" => 0x09,
        "g" => 0x0A,
        "h" => 0x0B,
        "i" => 0x0C,
        "j" => 0x0D,
        "k" => 0x0E,
        "l" => 0x0F,
        "m" => 0x10,
        "n" => 0x11,
        "o" => 0x12,
        "p" => 0x13,
        "q" => 0x14,
        "r" => 0x15,
        "s" => 0x16,
        "t" => 0x17,
        "u" => 0x18,
        "v" => 0x19,
        "w" => 0x1A,
        "x" => 0x1B,
        "y" => 0x1C,
        "z" => 0x1D,
        "1" => 0x1E,
        "2" => 0x1F,
        "3" => 0x20,
        "4" => 0x21,
        "5" => 0x22,
        "6" => 0x23,
        "7" => 0x24,
        "8" => 0x25,
        "9" => 0x26,
        "0" => 0x27,
        "enter" | "return" => 0x28,
        "esc" | "escape" => 0x29,
        "backspace" => 0x2A,
        "tab" => 0x2B,
        "space" => 0x2C,
        "-" | "minus" => 0x2D,
        "=" | "equal" => 0x2E,
        "[" => 0x2F,
        "]" => 0x30,
        "\\" => 0x31,
        ";" => 0x33,
        "'" => 0x34,
        "`" => 0x35,
        "," => 0x36,
        "." => 0x37,
        "/" => 0x38,
        "capslock" => 0x39,
        "f1" => 0x3A,
        "f2" => 0x3B,
        "f3" => 0x3C,
        "f4" => 0x3D,
        "f5" => 0x3E,
        "f6" => 0x3F,
        "f7" => 0x40,
        "f8" => 0x41,
        "f9" => 0x42,
        "f10" => 0x43,
        "f11" => 0x44,
        "f12" => 0x45,
        "printscreen" => 0x46,
        "scrolllock" => 0x47,
        "pause" => 0x48,
        "insert" => 0x49,
        "home" => 0x4A,
        "pageup" => 0x4B,
        "delete" => 0x4C,
        "end" => 0x4D,
        "pagedown" => 0x4E,
        "right" => 0x4F,
        "left" => 0x50,
        "down" => 0x51,
        "up" => 0x52,
        "numlock" => 0x53,
        "leftctrl" | "ctrl" => 0xE0,
        "leftshift" | "shift" => 0xE1,
        "leftalt" | "alt" => 0xE2,
        "leftmeta" | "win" | "cmd" | "super" => 0xE3,
        "rightctrl" => 0xE4,
        "rightshift" => 0xE5,
        "rightalt" => 0xE6,
        "rightmeta" => 0xE7,
        _ => return None,
    };
    Some(code)
}

/// 修饰键是否按下（用于组合键校验）。
pub fn is_modifier(scancode: u16) -> bool {
    (0xE0..=0xE7).contains(&scancode)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scancode_mapping_is_case_insensitive() {
        assert_eq!(scancode_for("A"), scancode_for("a"));
        assert_eq!(scancode_for("a"), Some(0x04));
        assert_eq!(scancode_for("Enter"), Some(0x28));
        assert_eq!(scancode_for("unknown-key"), None);
    }

    #[test]
    fn modifiers_recognized() {
        assert!(is_modifier(0xE0));
        assert!(is_modifier(0xE3));
        assert!(!is_modifier(0x04));
    }

    #[test]
    fn recording_injector_counts_events() {
        let mut inj = RecordingInjector::live();
        inj.inject(&InputEvent::Key {
            scancode: 0x04,
            pressed: true,
        })
        .unwrap();
        inj.inject(&InputEvent::MouseMove { x: 10, y: 20 }).unwrap();
        assert_eq!(inj.count(), 2);
        assert!(inj.is_live());
    }

    #[test]
    fn observer_mode_injects_nothing() {
        let mut inj = RecordingInjector::new(); // live = false
        inj.inject(&InputEvent::Key {
            scancode: 0x04,
            pressed: true,
        })
        .unwrap();
        assert_eq!(inj.count(), 0, "observer must not inject");
    }

    #[test]
    fn permission_denied_surfaces() {
        let mut inj = RecordingInjector::live();
        inj.deny = true;
        assert_eq!(
            inj.inject(&InputEvent::Key {
                scancode: 0x04,
                pressed: true
            }),
            Err(InjectError::PermissionDenied)
        );
    }

    #[test]
    fn coordinate_normalization_roundtrip() {
        let g = ScreenGeom {
            width: 1920,
            height: 1080,
        };
        let (nx, ny) = g.normalize(960, 540);
        let (x, y) = g.denormalize(nx, ny);
        assert!((x - 960).abs() <= 1, "x roundtrip {x}");
        assert!((y - 540).abs() <= 1, "y roundtrip {y}");
    }

    #[test]
    fn coordinate_bounds() {
        let g = ScreenGeom {
            width: 1920,
            height: 1080,
        };
        assert!(g.contains(0, 0));
        assert!(g.contains(1919, 1079));
        assert!(!g.contains(1920, 1079));
        assert!(!g.contains(-1, 0));
    }

    #[test]
    fn denormalize_reaches_edges() {
        let g = ScreenGeom {
            width: 1920,
            height: 1080,
        };
        assert_eq!(g.denormalize(0, 0), (0, 0));
        let (x, y) = g.denormalize(65535, 65535);
        assert!(x <= 1920 && y <= 1080);
    }
}
