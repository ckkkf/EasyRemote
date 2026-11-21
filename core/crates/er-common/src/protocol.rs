//! 协议消息定义，对应 docs/spec/protocol.md。
//! 只定义消息结构，传输实现（WebTransport）在 er-transport。

use serde::{Deserialize, Serialize};

/// 控制通道消息类型（与协议草案 §4 对齐）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum MsgType {
    Hello = 0x01,
    HelloAck = 0x02,
    QueryDevice = 0x03,
    DeviceStatus = 0x04,
    RequestConnect = 0x05,
    ConnectIncoming = 0x06,
    ConsentRequired = 0x07,
    SubmitCaptcha = 0x08,
    ConsentResult = 0x09,
    ConsentGranted = 0x0A,
    Candidates = 0x0B,
    Disconnect = 0x0C,

    PeerHello = 0x20,
    KeyExchange = 0x21,
    Caps = 0x22,
    StartStream = 0x23,
    InputEvent = 0x24,
    ClipboardUpdate = 0x25,
    PrivacyScreen = 0x26,
    DisplayList = 0x27,
    QualityReport = 0x28,
    ControlReq = 0x29,
    Bye = 0x2A,
}

impl MsgType {
    pub fn from_u8(v: u8) -> Option<Self> {
        use MsgType::*;
        Some(match v {
            0x01 => Hello,
            0x02 => HelloAck,
            0x03 => QueryDevice,
            0x04 => DeviceStatus,
            0x05 => RequestConnect,
            0x06 => ConnectIncoming,
            0x07 => ConsentRequired,
            0x08 => SubmitCaptcha,
            0x09 => ConsentResult,
            0x0A => ConsentGranted,
            0x0B => Candidates,
            0x0C => Disconnect,
            0x20 => PeerHello,
            0x21 => KeyExchange,
            0x22 => Caps,
            0x23 => StartStream,
            0x24 => InputEvent,
            0x25 => ClipboardUpdate,
            0x26 => PrivacyScreen,
            0x27 => DisplayList,
            0x28 => QualityReport,
            0x29 => ControlReq,
            0x2A => Bye,
            _ => return None,
        })
    }
}

/// 控制消息信封：magic(2) ver(1) type(1) flags(1) len(4) payload。
pub const MAGIC: [u8; 2] = *b"ER";
pub const VERSION: u8 = 0x01;
pub const ENVELOPE_HEADER_LEN: usize = 9;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Envelope {
    pub msg_type: u8,
    pub flags: u8,
    pub payload: Vec<u8>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EncodeError {
    PayloadTooLarge,
}

impl Envelope {
    pub fn new(msg_type: MsgType, flags: u8, payload: Vec<u8>) -> Self {
        Self { msg_type: msg_type as u8, flags, payload }
    }

    /// 编码为字节流。
    pub fn encode(&self) -> Result<Vec<u8>, EncodeError> {
        if self.payload.len() > u32::MAX as usize {
            return Err(EncodeError::PayloadTooLarge);
        }
        let mut out = Vec::with_capacity(ENVELOPE_HEADER_LEN + self.payload.len());
        out.extend_from_slice(&MAGIC);
        out.push(VERSION);
        out.push(self.msg_type);
        out.push(self.flags);
        out.extend_from_slice(&(self.payload.len() as u32).to_be_bytes());
        out.extend_from_slice(&self.payload);
        Ok(out)
    }

    /// 从字节流解析；不完整或 magic/版本不符返回 None。
    pub fn decode(buf: &[u8]) -> Option<Self> {
        if buf.len() < ENVELOPE_HEADER_LEN {
            return None;
        }
        if buf[0..2] != MAGIC || buf[2] != VERSION {
            return None;
        }
        let msg_type = buf[3];
        let flags = buf[4];
        let len = u32::from_be_bytes([buf[5], buf[6], buf[7], buf[8]]) as usize;
        if buf.len() < ENVELOPE_HEADER_LEN + len {
            return None;
        }
        Some(Self {
            msg_type,
            flags,
            payload: buf[ENVELOPE_HEADER_LEN..ENVELOPE_HEADER_LEN + len].to_vec(),
        })
    }
}

/// 媒体帧头，对应协议草案 §2.2。
pub const MEDIA_HEADER_LEN: usize = 8;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MediaHeader {
    pub frame_type: u8,
    pub stream_id: u8,
    pub seq: u16,
    pub flags: u8,
    pub ts_ms: u32,
}

impl MediaHeader {
    pub const FLAG_KEYFRAME: u8 = 0b0000_0001;
    pub const FLAG_HAS_EXT: u8 = 0b0000_0010;
    pub const FLAG_FRAME_END: u8 = 0b0000_0100;

    pub fn encode(&self) -> [u8; MEDIA_HEADER_LEN] {
        let mut out = [0u8; MEDIA_HEADER_LEN];
        out[0] = self.frame_type;
        out[1] = self.stream_id;
        out[2..4].copy_from_slice(&self.seq.to_be_bytes());
        out[4] = self.flags;
        out[5..9.min(MEDIA_HEADER_LEN)].copy_from_slice(&self.ts_ms.to_be_bytes()[1..4]);
        out
    }

    pub fn decode(buf: &[u8]) -> Option<Self> {
        if buf.len() < MEDIA_HEADER_LEN {
            return None;
        }
        Some(Self {
            frame_type: buf[0],
            stream_id: buf[1],
            seq: u16::from_be_bytes([buf[2], buf[3]]),
            flags: buf[4],
            ts_ms: u32::from_be_bytes([0, buf[5], buf[6], buf[7]]),
        })
    }

    pub fn is_keyframe(&self) -> bool {
        self.flags & Self::FLAG_KEYFRAME != 0
    }
}

/// 输入事件，对应协议草案 §5。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum InputEvent {
    KeyDown { scancode: u32, modifiers: u32, ts_ms: u64 },
    KeyUp { scancode: u32, modifiers: u32, ts_ms: u64 },
    MouseMove { x: u16, y: u16, ts_ms: u64 },
    MouseButton { button: u8, pressed: bool, x: u16, y: u16, ts_ms: u64 },
    MouseWheel { delta: i16, ts_ms: u64 },
}

impl InputEvent {
    /// 键盘事件用扫描码而非字符，避免输入法冲突。
    pub fn is_keyboard(&self) -> bool {
        matches!(self, InputEvent::KeyDown { .. } | InputEvent::KeyUp { .. })
    }
}

/// 会话角色。MVP 只实现 Operator，协议字段必须存在。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Role {
    Operator,
    Observer,
}

impl Role {
    pub fn as_str(&self) -> &'static str {
        match self {
            Role::Operator => "operator",
            Role::Observer => "observer",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn envelope_roundtrip() {
        let env = Envelope::new(MsgType::Hello, 0b01, b"payload".to_vec());
        let bytes = env.encode().unwrap();
        let got = Envelope::decode(&bytes).unwrap();
        assert_eq!(got, env);
    }

    #[test]
    fn envelope_rejects_bad_magic() {
        let mut bytes = Envelope::new(MsgType::Hello, 0, vec![]).encode().unwrap();
        bytes[0] = b'X';
        assert!(Envelope::decode(&bytes).is_none());
    }

    #[test]
    fn envelope_rejects_truncated() {
        let bytes = Envelope::new(MsgType::Hello, 0, b"abcdef".to_vec()).encode().unwrap();
        assert!(Envelope::decode(&bytes[..bytes.len() - 2]).is_none());
    }

    #[test]
    fn envelope_rejects_short_header() {
        assert!(Envelope::decode(&[0u8; 4]).is_none());
    }

    #[test]
    fn media_header_roundtrip() {
        let h = MediaHeader {
            frame_type: 1,
            stream_id: 0,
            seq: 4242,
            flags: MediaHeader::FLAG_KEYFRAME,
            ts_ms: 1_700_000,
        };
        let bytes = h.encode();
        let got = MediaHeader::decode(&bytes).unwrap();
        assert_eq!(got.seq, 4242);
        assert!(got.is_keyframe());
    }

    #[test]
    fn msg_type_roundtrip() {
        for t in [MsgType::Hello, MsgType::InputEvent, MsgType::Bye] {
            assert_eq!(MsgType::from_u8(t as u8), Some(t));
        }
        assert_eq!(MsgType::from_u8(0xFF), None);
    }

    #[test]
    fn input_event_serde() {
        let ev = InputEvent::MouseMove { x: 100, y: 200, ts_ms: 1 };
        let json = serde_json::to_string(&ev).unwrap();
        assert!(json.contains("mouse_move"));
        let back: InputEvent = serde_json::from_str(&json).unwrap();
        assert_eq!(back, ev);
    }

    #[test]
    fn role_serializes_as_string() {
        assert_eq!(Role::Operator.as_str(), "operator");
        assert_eq!(Role::Observer.as_str(), "observer");
    }
}
