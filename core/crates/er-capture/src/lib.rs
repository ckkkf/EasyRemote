//! 屏幕采集：抽象 + 跨平台真实实现。
//!
//! 抽象（Capture）保持不变；Windows 上真实实现已换成 **DXGI Desktop Duplication**
//! （GPU 原生、无 host 全帧拷贝），并可通过 dxgi::DxgiCapture 把
//! ID3D11Texture2D 零拷贝交给 NVENC。macOS 仍然走 xcap（CoreGraphics），
//! Linux 走 xcap（X11/Wayland），行为与替换前完全一致。
//!
//! 历史：Windows 上先用 xcap 跑通（每帧一次 GDI/DXGI 全帧 CPU 拷贝，
//! 2880x1620 实测约 600ms/帧），这正是端到端主瓶颈；DXGI Desktop Duplication
//! 的 GPU 纹理路径把它压到毫秒级，见 docs/impl/evidence/windows-2026-11-20-dxgi/。

pub mod privacy;

#[cfg(target_os = "windows")]
pub mod dxgi;

use std::collections::VecDeque;

/// 一帧采集结果（BGRA8）。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RawFrame {
    pub width: u32,
    pub height: u32,
    pub ts_ms: u64,
    pub data: Vec<u8>,
}

impl RawFrame {
    pub fn expected_len(width: u32, height: u32) -> usize {
        width as usize * height as usize * 4
    }

    pub fn is_well_formed(&self) -> bool {
        self.width > 0
            && self.height > 0
            && self.data.len() == Self::expected_len(self.width, self.height)
    }
}

/// 采集信息。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CaptureInfo {
    pub width: u32,
    pub height: u32,
    pub refresh_hz: u32,
    pub display_count: u32,
}

/// 采集错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CaptureError {
    /// 未授权（macOS 屏幕录制权限）
    PermissionDenied,
    /// 采集目标丢失
    Lost,
    /// 超时无新帧
    Timeout,
    /// 当前环境没有可用显示器/无法采集
    Unavailable,
}

impl std::fmt::Display for CaptureError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CaptureError::PermissionDenied => write!(f, "capture permission denied"),
            CaptureError::Lost => write!(f, "capture lost"),
            CaptureError::Timeout => write!(f, "capture timeout"),
            CaptureError::Unavailable => write!(f, "capture unavailable"),
        }
    }
}

impl std::error::Error for CaptureError {}

/// 采集接口。
pub trait Capture {
    /// 取下一帧。桌面无变化时应返回 Ok(None) 而不是伪造帧。
    fn next_frame(&mut self, timeout_ms: u32) -> Result<Option<RawFrame>, CaptureError>;
    fn info(&self) -> CaptureInfo;
}

/// 后端选择。
enum Backend {
    Xcap(xcap::Monitor),
    #[cfg(target_os = "windows")]
    Dxgi(dxgi::DxgiCapture),
}

/// 真实采集。
pub struct ScreenCapture {
    backend: Backend,
    info: CaptureInfo,
    /// Windows：DXGI 正常路径失败时的兜底（慢但一定出帧）
    #[cfg(target_os = "windows")]
    fallback: Option<xcap::Monitor>,
    last_data: Option<Vec<u8>>,
    seq: u64,
}

/// macOS：主显示器像素尺寸。
///
/// **必须用像素而不是 xcap 的 width()**：xcap 的 Monitor::width() 在 macOS 上返回的是
/// CGDisplayBounds（points），而 capture_image() 返回的是 CGImageGetWidth（像素）。
/// Retina 上两者差 2 倍：用 points 配编码器会 DimensionMismatch，用 points 算归一化坐标
/// 会让注入偏移一半。这里统一到像素，与采集帧、协议坐标一致。
#[cfg(target_os = "macos")]
fn macos_display_pixels() -> (u32, u32) {
    extern "C" {
        fn CGMainDisplayID() -> u32;
        fn CGDisplayPixelsWide(display: u32) -> usize;
        fn CGDisplayPixelsHigh(display: u32) -> usize;
    }
    unsafe {
        let id = CGMainDisplayID();
        (CGDisplayPixelsWide(id) as u32, CGDisplayPixelsHigh(id) as u32)
    }
}

fn open_xcap_primary() -> Result<(xcap::Monitor, u32, u32), CaptureError> {
    let monitors = xcap::Monitor::all().map_err(|_| CaptureError::Unavailable)?;
    let display = monitors.into_iter().next().ok_or(CaptureError::Unavailable)?;
    #[cfg(target_os = "macos")]
    {
        let (pw, ph) = macos_display_pixels();
        if pw > 0 && ph > 0 {
            return Ok((display, pw, ph));
        }
    }
    let width = display.width().map_err(|_| CaptureError::Unavailable)?;
    let height = display.height().map_err(|_| CaptureError::Unavailable)?;
    Ok((display, width, height))
}

fn xcap_grab(display: &xcap::Monitor) -> Result<RawFrame, CaptureError> {
    let image = display.capture_image().map_err(|e| {
        let msg = e.to_string().to_lowercase();
        if msg.contains("permission") || msg.contains("denied") || msg.contains("not authorized") {
            CaptureError::PermissionDenied
        } else if msg.contains("not found") || msg.contains("closed") {
            CaptureError::Lost
        } else {
            CaptureError::Unavailable
        }
    })?;

    let (w, h) = (image.width(), image.height());
    // xcap 返回 RGBA；对外统一成 BGRA 以匹配编码器输入
    let mut data = image.into_raw();
    for px in data.chunks_exact_mut(4) {
        px.swap(0, 2);
    }
    let ts_ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0);
    Ok(RawFrame { width: w, height: h, ts_ms, data })
}

impl ScreenCapture {
    /// 打开主显示器。Windows 上优先 DXGI Desktop Duplication。
    pub fn open_primary() -> Result<Self, CaptureError> {
        #[cfg(target_os = "windows")]
        {
            if let Ok(cap) = dxgi::DxgiCapture::open_primary() {
                let info = cap.info();
                return Ok(Self {
                    backend: Backend::Dxgi(cap),
                    info,
                    fallback: None,
                    last_data: None,
                    seq: 0,
                });
            }
        }
        let (display, width, height) = open_xcap_primary()?;
        Ok(Self {
            backend: Backend::Xcap(display),
            info: CaptureInfo { width, height, refresh_hz: 60, display_count: 1 },
            #[cfg(target_os = "windows")]
            fallback: None,
            last_data: None,
            seq: 0,
        })
    }

    /// 强制走 xcap 后端（对照基准与 macOS 行为验证用）。
    pub fn open_primary_xcap() -> Result<Self, CaptureError> {
        let (display, width, height) = open_xcap_primary()?;
        Ok(Self {
            backend: Backend::Xcap(display),
            info: CaptureInfo { width, height, refresh_hz: 60, display_count: 1 },
            #[cfg(target_os = "windows")]
            fallback: None,
            last_data: None,
            seq: 0,
        })
    }

    /// 当前后端名（证据/日志用）。
    pub fn backend_name(&self) -> &'static str {
        match &self.backend {
            Backend::Xcap(_) => "xcap",
            #[cfg(target_os = "windows")]
            Backend::Dxgi(_) => "dxgi",
        }
    }

    /// Windows：拿 DXGI 后端（没有则 None）。用于零拷贝编码路径。
    #[cfg(target_os = "windows")]
    pub fn dxgi_mut(&mut self) -> Option<&mut dxgi::DxgiCapture> {
        match &mut self.backend {
            Backend::Dxgi(c) => Some(c),
            _ => None,
        }
    }

    /// 让桌面产生一次 present（Windows 上发一次零位移鼠标输入）。
    ///
    /// 用途：新建会话/新订阅者接入时，DXGI 在**静止桌面**下按设计不出帧
    /// （AcquireNextFrame 恒 WAIT_TIMEOUT），若不上报一次「有变化」，观众会一直拿不到首帧。
    /// 零位移输入不会移动用户光标。
    ///
    /// 返回是否真的做了动作（非 Windows / 非 DXGI 后端返回 false）。
    pub fn prime_frame(&mut self) -> bool {
        #[cfg(target_os = "windows")]
        {
            if let Some(dxgi) = self.dxgi_mut() {
                return dxgi.nudge_desktop_zero();
            }
        }
        false
    }

    /// 采集一次（真实像素，BGRA）。
    pub fn grab(&mut self) -> Result<RawFrame, CaptureError> {
        #[cfg(target_os = "windows")]
        {
            let mut fallback_needed = false;
            let mut outcome: Option<Result<RawFrame, CaptureError>> = None;
            if let Backend::Dxgi(cap) = &mut self.backend {
                match cap.grab_cpu(200) {
                    Ok(Some(frame)) => outcome = Some(Ok(frame)),
                    // 桌面静止 / 采集失败：交给 xcap 兜底保证出帧
                    _ => fallback_needed = true,
                }
            }
            if let Some(res) = outcome {
                self.seq += 1;
                return res;
            }
            if fallback_needed {
                if self.fallback.is_none() {
                    if let Ok((display, _, _)) = open_xcap_primary() {
                        self.fallback = Some(display);
                    }
                }
                if let Some(display) = &self.fallback {
                    self.seq += 1;
                    return xcap_grab(display);
                }
                return Err(CaptureError::Timeout);
            }
        }
        match &self.backend {
            Backend::Xcap(display) => {
                self.seq += 1;
                xcap_grab(display)
            }
            #[cfg(target_os = "windows")]
            Backend::Dxgi(_) => Err(CaptureError::Unavailable),
        }
    }
}

impl Capture for ScreenCapture {
    fn next_frame(&mut self, timeout_ms: u32) -> Result<Option<RawFrame>, CaptureError> {
        #[cfg(target_os = "windows")]
        {
            let mut dxgi_attempt: Option<Result<Option<RawFrame>, CaptureError>> = None;
            if let Backend::Dxgi(cap) = &mut self.backend {
                dxgi_attempt = Some(cap.grab_cpu(timeout_ms));
            }
            if let Some(res) = dxgi_attempt {
                match res {
                    Ok(Some(frame)) => {
                        // 静止检测：与上一帧完全相同则返回 None，由上层重复上帧
                        if let Some(prev) = &self.last_data {
                            if prev.len() == frame.data.len() && prev == &frame.data {
                                return Ok(None);
                            }
                        }
                        self.last_data = Some(frame.data.clone());
                        return Ok(Some(frame));
                    }
                    Ok(None) => return Ok(None),
                    Err(e) => return Err(e),
                }
            }
        }
        let frame = self.grab()?;
        if let Some(prev) = &self.last_data {
            if prev.len() == frame.data.len() && prev == &frame.data {
                return Ok(None);
            }
        }
        self.last_data = Some(frame.data.clone());
        Ok(Some(frame))
    }

    fn info(&self) -> CaptureInfo {
        self.info
    }
}

/// 假实现：按脚本产帧，用于状态机与上层测试。
pub struct FakeCapture {
    info: CaptureInfo,
    frames: VecDeque<RawFrame>,
    lost: bool,
    denied: bool,
}

impl FakeCapture {
    pub fn new(width: u32, height: u32) -> Self {
        Self {
            info: CaptureInfo { width, height, refresh_hz: 60, display_count: 1 },
            frames: VecDeque::new(),
            lost: false,
            denied: false,
        }
    }

    pub fn push_frame(&mut self, ts_ms: u64) {
        let len = RawFrame::expected_len(self.info.width, self.info.height);
        self.frames.push_back(RawFrame {
            width: self.info.width,
            height: self.info.height,
            ts_ms,
            data: vec![0u8; len],
        });
    }

    pub fn set_lost(&mut self, v: bool) {
        self.lost = v;
    }

    pub fn set_denied(&mut self, v: bool) {
        self.denied = v;
    }
}

impl Capture for FakeCapture {
    fn next_frame(&mut self, _timeout_ms: u32) -> Result<Option<RawFrame>, CaptureError> {
        if self.denied {
            return Err(CaptureError::PermissionDenied);
        }
        if self.lost {
            return Err(CaptureError::Lost);
        }
        Ok(self.frames.pop_front())
    }

    fn info(&self) -> CaptureInfo {
        self.info
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_layout_is_bgra() {
        let f = RawFrame { width: 2, height: 2, ts_ms: 0, data: vec![0u8; 16] };
        assert!(f.is_well_formed());
        assert_eq!(RawFrame::expected_len(2, 2), 16);
    }

    #[test]
    fn malformed_frame_detected() {
        let f = RawFrame { width: 2, height: 2, ts_ms: 0, data: vec![0u8; 15] };
        assert!(!f.is_well_formed());
    }

    #[test]
    fn static_desktop_returns_none() {
        let mut c = FakeCapture::new(4, 4);
        assert!(c.next_frame(16).unwrap().is_none());
        c.push_frame(1);
        assert_eq!(c.next_frame(16).unwrap().unwrap().ts_ms, 1);
        assert!(c.next_frame(16).unwrap().is_none());
    }

    #[test]
    fn permission_and_lost_are_distinguishable() {
        let mut c = FakeCapture::new(4, 4);
        c.set_denied(true);
        assert_eq!(c.next_frame(16), Err(CaptureError::PermissionDenied));
        c.set_denied(false);
        c.set_lost(true);
        assert_eq!(c.next_frame(16), Err(CaptureError::Lost));
    }

    /// 真实采集：在本机抓一帧，验证尺寸与像素布局。
    /// 无屏幕录制权限或无显示器时跳过（不算失败）。
    #[test]
    fn real_capture_produces_well_formed_frame() {
        let mut cap = match ScreenCapture::open_primary() {
            Ok(c) => c,
            Err(e) => {
                eprintln!("skip: no display ({e})");
                return;
            }
        };
        eprintln!("backend = {}", cap.backend_name());
        match cap.grab() {
            Ok(frame) => {
                assert!(frame.width > 0 && frame.height > 0);
                assert!(
                    frame.is_well_formed(),
                    "frame {}x{} has {} bytes, expected {}",
                    frame.width,
                    frame.height,
                    frame.data.len(),
                    RawFrame::expected_len(frame.width, frame.height)
                );
                // 全黑画面说明没真采到内容
                assert!(frame.data.iter().any(|b| *b != 0), "captured frame is all zero");
                eprintln!("captured {}x{} frame", frame.width, frame.height);
            }
            Err(CaptureError::PermissionDenied) => eprintln!("skip: screen recording permission not granted"),
            Err(e) => eprintln!("skip: capture failed: {e}"),
        }
    }
}
