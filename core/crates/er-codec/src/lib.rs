//! 编解码：抽象 + 基于 OpenH264 的真实实现。
//!
//! 约束（PRD §5.2.3）：低延迟预设 —— 无 B 帧、GOP ≤ 1s、可按需请求关键帧。
//! OpenH264 默认就是无 B 帧的 baseline/main，天然满足低延迟前提。
//!
//! 说明：OpenH264 是软件编码。生产要冲 40ms 预算与 1080p60 时应换成
//! 硬件编码（NVENC/QSV/AMF/VideoToolbox），`Encoder` 接口不变。

#[cfg(target_os = "windows")]
pub mod nvenc;

use er_common::protocol::MediaHeader;
use openh264::decoder::Decoder;
use openh264::encoder::Encoder as H264Encoder;
use openh264::formats::{RgbaSliceU8, YUVBuffer, YUVSource};

/// 编码器种类。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EncoderKind {
    Nvenc,
    QuickSync,
    Amf,
    VideoToolbox,
    MediaCodec,
    /// OpenH264 软件编码（当前默认真实实现）
    Software,
}

impl EncoderKind {
    pub fn is_hardware(self) -> bool {
        !matches!(self, EncoderKind::Software)
    }
}

/// 编码参数。默认值即低延迟预设。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct EncodeConfig {
    pub width: u32,
    pub height: u32,
    pub fps: u32,
    pub bitrate_kbps: u32,
    pub gop: u32,
    pub bframes: u32,
}

impl Default for EncodeConfig {
    fn default() -> Self {
        Self { width: 1920, height: 1080, fps: 60, bitrate_kbps: 4000, gop: 60, bframes: 0 }
    }
}

impl EncodeConfig {
    pub fn is_low_latency(&self) -> bool {
        self.bframes == 0 && self.gop <= self.fps.max(1)
    }
}

/// 编码后的包。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Packet {
    pub header: MediaHeader,
    pub data: Vec<u8>,
}

/// 编解码错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CodecError {
    Unavailable,
    UnsupportedConfig,
    DeviceLost,
    /// 输入帧尺寸与配置不符
    DimensionMismatch,
    /// 编解码库内部错误
    Internal(String),
}

impl std::fmt::Display for CodecError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CodecError::Unavailable => write!(f, "codec unavailable"),
            CodecError::UnsupportedConfig => write!(f, "unsupported config"),
            CodecError::DeviceLost => write!(f, "codec device lost"),
            CodecError::DimensionMismatch => write!(f, "dimension mismatch"),
            CodecError::Internal(m) => write!(f, "codec error: {m}"),
        }
    }
}

impl std::error::Error for CodecError {}

/// 编码器接口。
pub trait Encoder {
    fn kind(&self) -> EncoderKind;
    fn encode(&mut self, frame_ts_ms: u64, frame: &[u8]) -> Result<Packet, CodecError>;
    fn request_keyframe(&mut self);
}

/// 基于 OpenH264 的真实编码器。
pub struct OpenH264Codec {
    cfg: EncodeConfig,
    /// 编码器必须跨帧复用：每帧重建会重复初始化，实测慢 10 倍以上。
    enc: H264Encoder,
    /// 已编码帧数递增，用于序号
    seq: u16,
    next_keyframe: bool,
    /// 强制 I 帧的间隔（帧）
    force_intra_every: u32,
    encoded_since_intra: u32,
}

impl OpenH264Codec {
    pub fn new(cfg: EncodeConfig) -> Result<Self, CodecError> {
        if cfg.width == 0 || cfg.height == 0 || cfg.fps == 0 {
            return Err(CodecError::UnsupportedConfig);
        }
        if !cfg.is_low_latency() {
            return Err(CodecError::UnsupportedConfig);
        }
        // 关键：默认 enable_skip_frame=true，码控会主动丢帧，
        // 表现是 encode 返回空 bitstream，上层再把它当空消息发出去
        // （实测导致「发 5 帧只收到 2 帧」）。实时屏幕共享必须关掉跳帧。
        let enc_cfg = openh264::encoder::EncoderConfig::new()
            .enable_skip_frame(false)
            .set_bitrate_bps(cfg.bitrate_kbps.saturating_mul(1000))
            .max_frame_rate(cfg.fps as f32)
            .usage_type(openh264::encoder::UsageType::ScreenContentRealTime)
            .rate_control_mode(openh264::encoder::RateControlMode::Bitrate);
        let enc = H264Encoder::with_api_config(openh264::OpenH264API::from_source(), enc_cfg)
            .map_err(|e| CodecError::Internal(e.to_string()))?;
        Ok(Self {
            cfg,
            enc,
            seq: 0,
            next_keyframe: true,
            force_intra_every: cfg.gop.max(1),
            encoded_since_intra: 0,
        })
    }

    pub fn config(&self) -> EncodeConfig {
        self.cfg
    }

    /// 编码一帧 BGRA 数据，返回 Annex-B 字节流。
    pub fn encode_bgra(&mut self, frame: &[u8], _ts_ms: u64) -> Result<Vec<u8>, CodecError> {
        let expected = (self.cfg.width as usize) * (self.cfg.height as usize) * 4;
        if frame.len() != expected {
            return Err(CodecError::DimensionMismatch);
        }

        let src = RgbaSliceU8::new(frame, (self.cfg.width as usize, self.cfg.height as usize));
        let yuv = YUVBuffer::from_rgb_source(src);
        let bitstream = self
            .enc
            .encode(&yuv)
            .map_err(|e| CodecError::Internal(e.to_string()))?;
        Ok(bitstream.to_vec())
    }
}

impl Encoder for OpenH264Codec {
    fn kind(&self) -> EncoderKind {
        EncoderKind::Software
    }

    fn encode(&mut self, frame_ts_ms: u64, frame: &[u8]) -> Result<Packet, CodecError> {
        let data = self.encode_bgra(frame, frame_ts_ms)?;
        let is_key = self.next_keyframe || self.encoded_since_intra >= self.force_intra_every;
        if is_key {
            self.next_keyframe = false;
            self.encoded_since_intra = 0;
        } else {
            self.encoded_since_intra += 1;
        }
        let mut flags = MediaHeader::FLAG_FRAME_END;
        if is_key {
            flags |= MediaHeader::FLAG_KEYFRAME;
        }
        let header = MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq: self.seq,
            flags,
            ts_ms: (frame_ts_ms & 0xFFFF_FFFF) as u32,
        };
        self.seq = self.seq.wrapping_add(1);
        Ok(Packet { header, data })
    }

    fn request_keyframe(&mut self) {
        self.next_keyframe = true;
        self.encoded_since_intra = 0;
    }
}

/// 按平台能力选编码器：Windows 上先试 NVENC（真开一次会话），
/// 不可用再退回 OpenH264 软件编码。其它平台直接软件编码。
pub fn create_encoder(cfg: EncodeConfig) -> Result<Box<dyn Encoder>, CodecError> {
    #[cfg(target_os = "windows")]
    {
        if let Ok(enc) = nvenc::NvencEncoder::new(cfg) {
            return Ok(Box::new(enc));
        }
    }
    Ok(Box::new(OpenH264Codec::new(cfg)?))
}

/// 硬件编码是否在本机可用（Windows/NVIDIA 才有意义，其它平台恒 false）。
pub fn hardware_encoder_available() -> bool {
    #[cfg(target_os = "windows")]
    {
        return nvenc::probe().available;
    }
    #[allow(unreachable_code)]
    false
}

/// 解码器：把 Annex-B 解回 YUV/RGB。
pub struct H264Decoder {
    decoder: Decoder,
}

impl H264Decoder {
    pub fn new() -> Result<Self, CodecError> {
        let decoder = Decoder::new().map_err(|e| CodecError::Internal(e.to_string()))?;
        Ok(Self { decoder })
    }

    /// 解一帧，返回 RGB 像素与尺寸。
    pub fn decode(&mut self, data: &[u8]) -> Result<Option<(usize, usize, Vec<u8>)>, CodecError> {
        let yuv = self.decoder.decode(data).map_err(|e| CodecError::Internal(e.to_string()))?;
        let Some(yuv) = yuv else { return Ok(None) };
        let (w, h) = yuv.dimensions();
        let mut rgb = vec![0u8; yuv.estimate_rgb_u8_size()];
        yuv.write_rgb8(&mut rgb);
        Ok(Some((w, h, rgb)))
    }
}

/// 假编码器：按固定比例产出压缩数据，用于上层与统计测试。
#[derive(Debug)]
pub struct FakeEncoder {
    cfg: EncodeConfig,
    kind: EncoderKind,
    seq: u16,
    next_keyframe: bool,
    fail_next: bool,
}

impl FakeEncoder {
    pub fn new(cfg: EncodeConfig, kind: EncoderKind) -> Result<Self, CodecError> {
        if cfg.width == 0 || cfg.height == 0 || cfg.fps == 0 || !cfg.is_low_latency() {
            return Err(CodecError::UnsupportedConfig);
        }
        Ok(Self { cfg, kind, seq: 0, next_keyframe: true, fail_next: false })
    }
    pub fn fail_next(&mut self, v: bool) {
        self.fail_next = v;
    }
}

impl Encoder for FakeEncoder {
    fn kind(&self) -> EncoderKind {
        self.kind
    }

    fn encode(&mut self, frame_ts_ms: u64, pixel_len: &[u8]) -> Result<Packet, CodecError> {
        if self.fail_next {
            self.fail_next = false;
            return Err(CodecError::DeviceLost);
        }
        let is_key = self.next_keyframe;
        self.next_keyframe = false;
        let mut flags = MediaHeader::FLAG_FRAME_END;
        if is_key {
            flags |= MediaHeader::FLAG_KEYFRAME;
        }
        let div = if is_key { 10 } else { 200 };
        let len = (pixel_len.len() / div).max(1);
        let header = MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq: self.seq,
            flags,
            ts_ms: (frame_ts_ms & 0xFFFF_FFFF) as u32,
        };
        self.seq = self.seq.wrapping_add(1);
        Ok(Packet { header, data: vec![0u8; len] })
    }

    fn request_keyframe(&mut self) {
        self.next_keyframe = true;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(w: u32, h: u32) -> EncodeConfig {
        EncodeConfig { width: w, height: h, ..Default::default() }
    }

    #[test]
    fn default_config_is_low_latency() {
        assert!(EncodeConfig::default().is_low_latency());
    }

    #[test]
    fn bframes_violate_low_latency() {
        let c = EncodeConfig { bframes: 2, ..Default::default() };
        assert!(!c.is_low_latency());
        assert_eq!(FakeEncoder::new(c, EncoderKind::Software).unwrap_err(), CodecError::UnsupportedConfig);
    }

    #[test]
    fn dimension_mismatch_rejected() {
        let mut codec = OpenH264Codec::new(cfg(64, 64)).unwrap();
        assert_eq!(codec.encode_bgra(&[0u8; 16], 0), Err(CodecError::DimensionMismatch));
    }

    /// 真实编码：喂一帧渐变图，必须产出非空 H.264 数据。
    #[test]
    fn real_h264_encodes_frame() {
        let (w, h) = (320u32, 240u32);
        let mut codec = OpenH264Codec::new(cfg(w, h)).unwrap();
        let mut frame = vec![0u8; (w * h * 4) as usize];
        for y in 0..h {
            for x in 0..w {
                let i = ((y * w + x) * 4) as usize;
                frame[i] = (x % 256) as u8;
                frame[i + 1] = (y % 256) as u8;
                frame[i + 2] = 128;
                frame[i + 3] = 255;
            }
        }
        let pkt = codec.encode_bgra(&frame, 16).unwrap();
        assert!(!pkt.is_empty(), "encoder produced no data");
        // Annex-B 起始码
        assert!(
            pkt.starts_with(&[0, 0, 0, 1]) || pkt.starts_with(&[0, 0, 1]),
            "output is not Annex-B: {:?}",
            &pkt[..pkt.len().min(8)]
        );
        eprintln!("encoded {}x{} frame -> {} bytes", w, h, pkt.len());
    }

    /// 真实往返：编码 → 解码，尺寸一致且像素非全零。
    #[test]
    fn real_h264_roundtrip() {
        let (w, h) = (160u32, 120u32);
        let mut codec = OpenH264Codec::new(cfg(w, h)).unwrap();
        let mut frame = vec![0u8; (w * h * 4) as usize];
        for i in (0..frame.len()).step_by(4) {
            frame[i] = 200;
            frame[i + 1] = 60;
            frame[i + 2] = 30;
            frame[i + 3] = 255;
        }
        let encoded = codec.encode_bgra(&frame, 0).unwrap();

        let mut dec = H264Decoder::new().unwrap();
        let decoded = dec.decode(&encoded).unwrap().expect("decoder returned nothing");
        let (dw, dh, rgb) = decoded;
        assert_eq!((dw, dh), (w as usize, h as usize));
        assert!(rgb.iter().any(|b| *b != 0), "decoded pixels are all zero");
        eprintln!("roundtrip {}x{} -> {} bytes rgb", dw, dh, rgb.len());
    }
}
