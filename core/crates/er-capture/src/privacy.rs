//! 隐私屏（PRD §5.2.6）：被控端开启后，画面内容不离开本机。
//!
//! 实现方式：采到帧之后、编码之前把像素清成纯色（默认黑）。
//! 这样即使数据被截获也没有信息，且与主控端行为无关。
//!
//! 注意：**不是**在显示层盖一层窗口（那样截屏仍能拿到内容）。

use crate::RawFrame;

/// 隐私屏纯色（BGRA）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PrivacyColor {
    pub b: u8,
    pub g: u8,
    pub r: u8,
    pub a: u8,
}

impl Default for PrivacyColor {
    fn default() -> Self {
        Self {
            b: 0,
            g: 0,
            r: 0,
            a: 255,
        } // 黑
    }
}

/// 把帧内容抹成纯色。返回被覆盖的帧。
pub fn apply_privacy(frame: &mut RawFrame, color: PrivacyColor) {
    for px in frame.data.chunks_exact_mut(4) {
        px[0] = color.b;
        px[1] = color.g;
        px[2] = color.r;
        px[3] = color.a;
    }
}

/// 判断一帧是否已完全被抹平（用于自检：开启隐私屏后不得漏像素）。
pub fn is_uniform(frame: &RawFrame) -> bool {
    let mut it = frame.data.chunks_exact(4);
    let Some(first) = it.next() else { return true };
    it.all(|px| px == first)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn frame(w: u32, h: u32) -> RawFrame {
        RawFrame {
            width: w,
            height: h,
            ts_ms: 0,
            data: (0..(w * h * 4)).map(|i| (i % 251) as u8).collect(),
        }
    }

    #[test]
    fn privacy_blacks_out_every_pixel() {
        let mut f = frame(4, 4);
        assert!(!is_uniform(&f));
        apply_privacy(&mut f, PrivacyColor::default());
        assert!(is_uniform(&f), "all pixels must be identical after privacy");
        assert_eq!(&f.data[0..4], &[0, 0, 0, 255]);
    }

    #[test]
    fn frame_shape_preserved() {
        let mut f = frame(3, 2);
        let len = f.data.len();
        apply_privacy(&mut f, PrivacyColor::default());
        assert_eq!(f.data.len(), len, "privacy must not change frame size");
        assert_eq!((f.width, f.height), (3, 2));
    }

    #[test]
    fn no_residual_original_bytes() {
        let mut f = frame(8, 8);
        let original = f.data.clone();
        apply_privacy(
            &mut f,
            PrivacyColor {
                b: 10,
                g: 20,
                r: 30,
                a: 255,
            },
        );
        assert!(
            f.data.chunks_exact(4).all(|px| px == [10, 20, 30, 255]),
            "no pixel may retain original content"
        );
        assert_ne!(f.data, original);
    }
}
