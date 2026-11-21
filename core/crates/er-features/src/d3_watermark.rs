//! D3 会话水印：在被控端「采集后、编码前」叠加，使主控截图必然带水印。
//!
//! 本模块只做**布局计算**（纯逻辑，可完整单测）；
//! 真正的像素合成在平台渲染层完成（GPU）。

/// 水印规格。
#[derive(Debug, Clone, PartialEq)]
pub struct WatermarkSpec {
    pub text: String,
    /// 不透明度，建议 0.08–0.15
    pub opacity: f32,
    /// 倾斜角（度）
    pub angle_deg: f32,
    /// 字号（像素）
    pub font_px: u32,
    /// 平铺间距（像素）
    pub spacing_px: u32,
}

impl Default for WatermarkSpec {
    fn default() -> Self {
        Self {
            text: String::new(),
            opacity: 0.12,
            angle_deg: -30.0,
            font_px: 18,
            spacing_px: 220,
        }
    }
}

impl WatermarkSpec {
    /// 构造一份合规规格：不透明度被夹到 0.08–0.15，
    /// 低于 0.08 看不见（起不到追溯作用），高于 0.15 干扰使用（PRD §5.7）。
    pub fn new(text: impl Into<String>) -> Self {
        let mut s = Self::default();
        s.text = text.into();
        s.opacity = s.opacity.clamp(MIN_OPACITY, MAX_OPACITY);
        s
    }

    /// 规整参数，返回合规后的副本。
    pub fn normalized(&self) -> Self {
        let mut out = self.clone();
        out.opacity = out.opacity.clamp(MIN_OPACITY, MAX_OPACITY);
        out.font_px = out.font_px.clamp(10, 96);
        out.spacing_px = out.spacing_px.max(out.font_px * 4);
        out
    }
}

pub const MIN_OPACITY: f32 = 0.08;
pub const MAX_OPACITY: f32 = 0.15;

/// 一个水印平铺位置。
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct WatermarkTile {
    pub x: i32,
    pub y: i32,
    /// 该位置是否在可见区域内（边缘的瓦片只画一部分）
    pub visible: bool,
}

/// 计算覆盖整帧的平铺位置。
///
/// 采用「旋转后按方格铺满 + 外扩一圈」的策略，保证画面任意位置
/// 都落在某个水印的文字覆盖范围内（不留空白区，避免被裁剪掉水印）。
pub fn layout(spec: &WatermarkSpec, width: u32, height: u32) -> Vec<WatermarkTile> {
    let spec = spec.normalized();
    if width == 0 || height == 0 || spec.spacing_px == 0 {
        return Vec::new();
    }
    let step = spec.spacing_px as i32;
    // 旋转会让瓦片外扩，多铺一圈保证边缘也有水印
    let margin = step;
    let mut tiles = Vec::new();
    let mut y = -margin;
    while y <= height as i32 + margin {
        let mut x = -margin;
        while x <= width as i32 + margin {
            let visible = x >= 0 && y >= 0 && x < width as i32 && y < height as i32;
            tiles.push(WatermarkTile { x, y, visible });
            x += step;
        }
        y += step;
    }
    tiles
}

/// 统计真正落在画面内的瓦片数量，用于验收「无空白区」。
pub fn visible_tile_count(tiles: &[WatermarkTile]) -> usize {
    tiles.iter().filter(|t| t.visible).count()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opacity_is_clamped_to_safe_range() {
        let mut s = WatermarkSpec::new("user_82**").normalized();
        assert!(s.opacity >= MIN_OPACITY && s.opacity <= MAX_OPACITY);

        s.opacity = 0.5;
        assert_eq!(s.normalized().opacity, MAX_OPACITY);
        s.opacity = 0.001;
        assert_eq!(s.normalized().opacity, MIN_OPACITY);
    }

    #[test]
    fn spacing_never_smaller_than_four_fonts() {
        let mut s = WatermarkSpec::new("x");
        s.font_px = 40;
        s.spacing_px = 10;
        assert_eq!(s.normalized().spacing_px, 160);
    }

    #[test]
    fn tiles_cover_full_frame() {
        let s = WatermarkSpec::new("user_82** · 2026-11-20 09:00");
        let tiles = layout(&s, 1920, 1080);
        assert!(visible_tile_count(&tiles) > 0);
        // 每个像素点都应被某个间距网格覆盖：可见瓦片数 ≈ (w/step)*(h/step)
        let step = s.normalized().spacing_px as usize;
        let expected = (1920 / step) * (1080 / step);
        let actual = visible_tile_count(&tiles);
        assert!(
            actual >= expected,
            "expected at least {expected} visible tiles, got {actual}"
        );
    }

    #[test]
    fn tiles_extend_beyond_edges_so_no_gap() {
        let s = WatermarkSpec::new("x");
        let tiles = layout(&s, 100, 100);
        assert!(tiles.iter().any(|t| t.x < 0), "need out-of-frame margin");
        assert!(tiles.iter().any(|t| t.y < 0));
    }

    #[test]
    fn empty_frame_produces_no_tiles() {
        let s = WatermarkSpec::new("x");
        assert!(layout(&s, 0, 0).is_empty());
        assert!(layout(&s, 0, 1080).is_empty());
    }

    #[test]
    fn deterministic_layout() {
        let s = WatermarkSpec::new("user_82**");
        let a = layout(&s, 1280, 800);
        let b = layout(&s, 1280, 800);
        assert_eq!(a, b);
    }
}
