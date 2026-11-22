//! 中继线格式：与 Go `internal/relay` 逐字节兼容。
//!
//! 帧布局：type(u8) + sidLen(u16 BE) + sid + body
//! 注册体：devLen(u16 BE) + deviceID + ticket
//! 分片：  msgID(u32 BE) + idx(u16 BE) + total(u16 BE) + payload

use std::collections::HashMap;

/// 帧类型，必须与 Go 一致。
pub const FRAME_REGISTER: u8 = 1;
pub const FRAME_DATA: u8 = 2;
pub const FRAME_NODE_FORWARD: u8 = 3;
/// 中继确认注册成功；客户端必须收到它才算注册完成。
pub const FRAME_REGISTER_ACK: u8 = 4;

/// ACK 载荷魔数：客户端用它把“迟到的重复 ACK”从数据分片中过滤掉。
pub const REGISTER_ACK_MAGIC: &[u8] = b"ERACK1";

/// 单个 UDP 包上限。
///
/// macOS 的 UDP 载荷上限是 65507 字节，Go 侧写 64KB 会踩到
/// "Message too long (os error 40)"。这里取 32KB，留足余量，
/// 实际发送还会按 8KB 分片。
pub const MAX_DATAGRAM: usize = 32 * 1024;

/// 分片头长度。
pub const FRAG_HEADER: usize = 8;

/// 线格式错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WireError {
    Short,
    BadUtf8,
}

impl std::fmt::Display for WireError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            WireError::Short => write!(f, "short frame"),
            WireError::BadUtf8 => write!(f, "bad utf8"),
        }
    }
}

impl std::error::Error for WireError {}

/// 构造帧。
pub fn encode_frame(kind: u8, sid: &str, body: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(3 + sid.len() + body.len());
    out.push(kind);
    out.extend_from_slice(&(sid.len() as u16).to_be_bytes());
    out.extend_from_slice(sid.as_bytes());
    out.extend_from_slice(body);
    out
}

/// 解析帧。
pub fn parse_frame(buf: &[u8]) -> Result<(u8, String, &[u8]), WireError> {
    if buf.len() < 3 {
        return Err(WireError::Short);
    }
    let kind = buf[0];
    let n = u16::from_be_bytes([buf[1], buf[2]]) as usize;
    if buf.len() < 3 + n {
        return Err(WireError::Short);
    }
    let sid = std::str::from_utf8(&buf[3..3 + n]).map_err(|_| WireError::BadUtf8)?;
    Ok((kind, sid.to_string(), &buf[3 + n..]))
}

/// 构造注册体。
pub fn encode_register(device_id: &str, ticket: &str) -> Vec<u8> {
    let mut out = Vec::with_capacity(2 + device_id.len() + ticket.len());
    out.extend_from_slice(&(device_id.len() as u16).to_be_bytes());
    out.extend_from_slice(device_id.as_bytes());
    out.extend_from_slice(ticket.as_bytes());
    out
}

/// 解析注册体。
pub fn parse_register(body: &[u8]) -> Result<(String, String), WireError> {
    if body.len() < 2 {
        return Err(WireError::Short);
    }
    let n = u16::from_be_bytes([body[0], body[1]]) as usize;
    if body.len() < 2 + n {
        return Err(WireError::Short);
    }
    let dev = std::str::from_utf8(&body[2..2 + n]).map_err(|_| WireError::BadUtf8)?;
    let tk = std::str::from_utf8(&body[2 + n..]).map_err(|_| WireError::BadUtf8)?;
    Ok((dev.to_string(), tk.to_string()))
}

/// 分片：把一个大帧切成不超过 max_payload 的若干块。
pub fn fragment(msg_id: u32, data: &[u8], max_payload: usize) -> Vec<Vec<u8>> {
    let max = max_payload.max(1);
    let total = data.len().div_ceil(max).max(1);
    let mut out = Vec::with_capacity(total);
    for (i, piece) in data.chunks(max).enumerate() {
        let mut buf = Vec::with_capacity(FRAG_HEADER + piece.len());
        buf.extend_from_slice(&msg_id.to_be_bytes());
        buf.extend_from_slice(&(i as u16).to_be_bytes());
        buf.extend_from_slice(&(total as u16).to_be_bytes());
        buf.extend_from_slice(piece);
        out.push(buf);
    }
    if data.is_empty() {
        // 空载荷也要发一个 total=1 的片，否则接收方永远等不到数据
        let mut buf = vec![0u8; FRAG_HEADER];
        buf[0..4].copy_from_slice(&msg_id.to_be_bytes());
        buf[4..6].copy_from_slice(&0u16.to_be_bytes());
        buf[6..8].copy_from_slice(&1u16.to_be_bytes());
        out.push(buf);
    }
    out
}

#[derive(Default)]
struct Partial {
    total: usize,
    parts: Vec<Option<Vec<u8>>>,
    got: usize,
    age: u64,
    /// 首片到达时刻（毫秒，调用方给的本机钟）；用于超时清理时结算缺失分片
    created_ms: u64,
}

/// 分片级收发计数。
///
/// 为什么要有这个：视频帧是**多分片**消息，一个分片丢了整帧就废。
/// 按「帧」统计的丢帧率会被放大（丢 5% 数据报 ≈ 丢掉 20%+ 的帧），
/// 拿它喂 ABR 会把 5% 的链路判成重度拥塞。按分片算才反映真实链路。
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct ReassemblyStats {
    pub fragments_received: u64,
    pub fragments_missing: u64,
}

impl ReassemblyStats {
    pub fn loss_pct(&self) -> f32 {
        let total = self.fragments_received + self.fragments_missing;
        if total == 0 {
            0.0
        } else {
            (self.fragments_missing as f32) * 100.0 / (total as f32)
        }
    }
}

/// 同时在途的消息数上限。超出后淘汰最旧的（视频场景丢旧帧可接受）。
const MAX_INFLIGHT: usize = 8;

/// 单条消息分片数上限：防止恶意 total=65535 造成大分配。
const MAX_FRAGMENTS: usize = 4096;

/// 重组器：支持多消息并发，避免新消息打断未完成的上一条。
#[derive(Default)]
pub struct Reassembler {
    inflight: HashMap<u32, Partial>,
    tick: u64,
    stats: ReassemblyStats,
}

impl Reassembler {
    pub fn new() -> Self {
        Self::default()
    }

    /// 当前在途消息数（测试/诊断：必须有界）。
    pub fn inflight_len(&self) -> usize {
        self.inflight.len()
    }

    /// 分片级计数（不清理未完成消息）。
    pub fn stats(&self) -> ReassemblyStats {
        self.stats
    }

    /// 清理超过 `older_than_ms` 仍未补齐的消息，并把缺失分片记进统计。
    ///
    /// 不清理的话，丢包造成的洞永远停在 inflight 里，丢包率会被算成 0。
    /// 返回本次结算的缺失分片数。
    pub fn sweep_stale(&mut self, older_than_ms: u64, now_ms: u64) -> u64 {
        let mut missing = 0u64;
        let stale: Vec<u32> = self
            .inflight
            .iter()
            .filter(|(_, p)| now_ms.saturating_sub(p.created_ms) > older_than_ms)
            .map(|(k, _)| *k)
            .collect();
        for k in stale {
            if let Some(p) = self.inflight.remove(&k) {
                missing += (p.total - p.got) as u64;
            }
        }
        self.stats.fragments_missing += missing;
        missing
    }

    /// 喂一个分片；收齐返回 Some(完整数据)。
    pub fn push(&mut self, frag: &[u8]) -> Option<Vec<u8>> {
        if frag.len() < FRAG_HEADER {
            return None;
        }
        let msg_id = u32::from_be_bytes([frag[0], frag[1], frag[2], frag[3]]);
        let idx = u16::from_be_bytes([frag[4], frag[5]]) as usize;
        let total = u16::from_be_bytes([frag[6], frag[7]]) as usize;
        if total == 0 || idx >= total || total > MAX_FRAGMENTS {
            return None;
        }

        self.tick += 1;
        let tick = self.tick;

        if !self.inflight.contains_key(&msg_id) {
            if self.inflight.len() >= MAX_INFLIGHT {
                if let Some(oldest) = self
                    .inflight
                    .iter()
                    .min_by_key(|(_, p)| p.age)
                    .map(|(k, _)| *k)
                {
                    self.inflight.remove(&oldest);
                }
            }
            self.inflight.insert(
                msg_id,
                Partial {
                    total,
                    parts: vec![None; total],
                    got: 0,
                    age: tick,
                    created_ms: now_ms(),
                },
            );
        }

        let entry = self.inflight.get_mut(&msg_id)?;
        entry.age = tick;
        if entry.total != total {
            self.inflight.remove(&msg_id);
            return None;
        }
        if entry.parts[idx].is_none() {
            entry.parts[idx] = Some(frag[FRAG_HEADER..].to_vec());
            entry.got += 1;
            self.stats.fragments_received += 1;
        }
        if entry.got != entry.total {
            return None;
        }

        let done = self.inflight.remove(&msg_id)?;
        // 已收的分片在 push 里就计过；这里只补「本该有但没有」的部分
        self.stats.fragments_missing += (done.total - done.got) as u64;
        let mut out = Vec::new();
        for p in done.parts {
            out.extend_from_slice(&p?);
        }
        Some(out)
    }
}

fn now_ms() -> u64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_roundtrip() {
        let f = encode_frame(FRAME_DATA, "ses-abc", b"payload");
        let (kind, sid, body) = parse_frame(&f).unwrap();
        assert_eq!(kind, FRAME_DATA);
        assert_eq!(sid, "ses-abc");
        assert_eq!(body, b"payload");
    }

    /// 关键回归：Go 用长度前缀而非固定 36 字节，任意长度 sid 都要能过
    #[test]
    fn arbitrary_sid_length() {
        for len in [0usize, 1, 5, 8, 36, 64] {
            let sid = "s".repeat(len);
            let f = encode_frame(FRAME_REGISTER, &sid, b"x");
            let (_, got, _) = parse_frame(&f).unwrap();
            assert_eq!(got, sid);
        }
    }

    #[test]
    fn register_roundtrip() {
        let b = encode_register("dev-123", "ticket.value");
        let (dev, tk) = parse_register(&b).unwrap();
        assert_eq!(dev, "dev-123");
        assert_eq!(tk, "ticket.value");
    }

    #[test]
    fn short_frames_rejected() {
        assert_eq!(parse_frame(&[1, 0]), Err(WireError::Short));
        assert_eq!(parse_register(&[0]), Err(WireError::Short));
    }

    #[test]
    fn fragmentation_and_reassembly() {
        let data: Vec<u8> = (0..1000).map(|i| (i % 251) as u8).collect();
        let frags = fragment(7, &data, 256);
        assert_eq!(frags.len(), 4);
        let mut r = Reassembler::new();
        let mut out = None;
        for f in frags {
            if let Some(v) = r.push(&f) {
                out = Some(v);
            }
        }
        assert_eq!(out.unwrap(), data);
    }

    #[test]
    fn out_of_order_reassembly() {
        let data: Vec<u8> = (0..900).map(|i| (i % 97) as u8).collect();
        let mut frags = fragment(9, &data, 128);
        frags.reverse();
        let mut r = Reassembler::new();
        let mut out = None;
        for f in frags {
            if let Some(v) = r.push(&f) {
                out = Some(v);
            }
        }
        assert_eq!(out.unwrap(), data);
    }

    #[test]
    fn empty_payload_roundtrip() {
        let frags = fragment(1, &[], 64);
        let mut r = Reassembler::new();
        assert_eq!(r.push(&frags[0]), Some(Vec::new()));
    }
    #[test]
    fn oversized_fragment_count_rejected() {
        let mut r = Reassembler::new();
        let mut frag = vec![0u8; FRAG_HEADER];
        frag[0..4].copy_from_slice(&1u32.to_be_bytes());
        frag[4..6].copy_from_slice(&0u16.to_be_bytes());
        frag[6..8].copy_from_slice(&(MAX_FRAGMENTS as u16 + 1).to_be_bytes());
        assert!(r.push(&frag).is_none());
    }
    #[test]
    fn inflight_messages_are_bounded() {
        let mut r = Reassembler::new();
        for id in 0..(MAX_INFLIGHT as u32 + 5) {
            let mut frag = vec![0u8; FRAG_HEADER];
            frag[0..4].copy_from_slice(&id.to_be_bytes());
            frag[4..6].copy_from_slice(&0u16.to_be_bytes());
            frag[6..8].copy_from_slice(&2u16.to_be_bytes());
            assert!(r.push(&frag).is_none());
        }
        assert!(
            r.inflight_len() <= MAX_INFLIGHT,
            "inflight={}",
            r.inflight_len()
        );
    }
}
