//! 传输层抽象。
//!
//! 约束（PRD §7.2.2）：上层只能依赖本 crate 的 trait 与类型，
//! 不得直接依赖 quinn / wtransport 的具体类型，以便 V2 替换实现。

pub mod quic;

use er_common::protocol::MediaHeader;

/// 通道。视频/音频走不可靠数据报，控制/数据走可靠流。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Channel {
    Video,
    Audio,
    Control,
    Data,
}

impl Channel {
    /// 该通道是否要求可靠有序。
    pub fn is_reliable(self) -> bool {
        matches!(self, Channel::Control | Channel::Data)
    }
}

/// 传输错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TransportError {
    Closed,
    Backpressure,
    TooLarge,
}

impl std::fmt::Display for TransportError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TransportError::Closed => write!(f, "transport closed"),
            TransportError::Backpressure => write!(f, "transport backpressure"),
            TransportError::TooLarge => write!(f, "frame too large"),
        }
    }
}

impl std::error::Error for TransportError {}

/// 收到的一帧。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Frame {
    pub channel: Channel,
    pub header: Option<MediaHeader>,
    pub payload: Vec<u8>,
}

/// 传输统计，供质量浮层与后台指标使用。
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Stats {
    pub sent_datagrams: u64,
    pub sent_reliable: u64,
    pub dropped_datagrams: u64,
    pub recv_frames: u64,
    pub bytes_sent: u64,
    pub bytes_recv: u64,
}

/// 传输接口。实现方负责连接、打洞与中继切换。
pub trait Transport {
    /// 发送不可靠数据报（视频/音频）。队列满时丢最旧帧，绝不阻塞。
    fn send_datagram(&mut self, channel: Channel, header: MediaHeader, payload: Vec<u8>) -> Result<(), TransportError>;

    /// 发送可靠数据（控制/数据）。不允许丢弃，队列满返回 Backpressure。
    fn send_reliable(&mut self, channel: Channel, payload: Vec<u8>) -> Result<(), TransportError>;

    /// 取出下一帧（非阻塞）。
    fn recv(&mut self) -> Option<Frame>;

    /// 读取统计。
    fn stats(&self) -> Stats;

    /// 关闭传输。
    fn close(&mut self);
}

/// 内存实现：用于单元测试与协议对拍。
///
/// 丢帧策略与 PRD「帧不排队」一致：数据报队列满时丢最旧一帧并计入
/// `dropped_datagrams`；可靠通道满则返回 Backpressure。
pub struct MemoryTransport {
    datagram_queue: std::collections::VecDeque<Frame>,
    reliable_queue: std::collections::VecDeque<Frame>,
    capacity: usize,
    max_frame: usize,
    stats: Stats,
    closed: bool,
}

impl MemoryTransport {
    pub fn new(capacity: usize, max_frame: usize) -> Self {
        assert!(capacity > 0, "capacity must be positive");
        Self {
            datagram_queue: std::collections::VecDeque::with_capacity(capacity),
            reliable_queue: std::collections::VecDeque::with_capacity(capacity),
            capacity,
            max_frame,
            stats: Stats::default(),
            closed: false,
        }
    }

    /// 当前待发帧数，供测试观察背压行为。
    pub fn pending(&self) -> (usize, usize) {
        (self.datagram_queue.len(), self.reliable_queue.len())
    }
}

impl Transport for MemoryTransport {
    fn send_datagram(&mut self, channel: Channel, header: MediaHeader, payload: Vec<u8>) -> Result<(), TransportError> {
        if self.closed {
            return Err(TransportError::Closed);
        }
        if channel.is_reliable() {
            return Err(TransportError::Backpressure);
        }
        if payload.len() > self.max_frame {
            return Err(TransportError::TooLarge);
        }
        if self.datagram_queue.len() >= self.capacity {
            self.datagram_queue.pop_front();
            self.stats.dropped_datagrams += 1;
        }
        self.stats.bytes_sent += payload.len() as u64;
        self.stats.sent_datagrams += 1;
        self.datagram_queue.push_back(Frame { channel, header: Some(header), payload });
        Ok(())
    }

    fn send_reliable(&mut self, channel: Channel, payload: Vec<u8>) -> Result<(), TransportError> {
        if self.closed {
            return Err(TransportError::Closed);
        }
        if !channel.is_reliable() {
            return Err(TransportError::Backpressure);
        }
        if payload.len() > self.max_frame {
            return Err(TransportError::TooLarge);
        }
        if self.reliable_queue.len() >= self.capacity {
            return Err(TransportError::Backpressure);
        }
        self.stats.bytes_sent += payload.len() as u64;
        self.stats.sent_reliable += 1;
        self.reliable_queue.push_back(Frame { channel, header: None, payload });
        Ok(())
    }

    fn recv(&mut self) -> Option<Frame> {
        // 可靠通道优先，避免控制指令被视频帧饿死
        let frame = self.reliable_queue.pop_front().or_else(|| self.datagram_queue.pop_front())?;
        self.stats.recv_frames += 1;
        self.stats.bytes_recv += frame.payload.len() as u64;
        Some(frame)
    }

    fn stats(&self) -> Stats {
        self.stats
    }

    fn close(&mut self) {
        self.closed = true;
        self.datagram_queue.clear();
        self.reliable_queue.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hdr(seq: u16) -> MediaHeader {
        MediaHeader { frame_type: 1, stream_id: 0, seq, flags: 0, ts_ms: 0 }
    }

    #[test]
    fn channel_reliability() {
        assert!(Channel::Control.is_reliable());
        assert!(Channel::Data.is_reliable());
        assert!(!Channel::Video.is_reliable());
        assert!(!Channel::Audio.is_reliable());
    }

    #[test]
    fn datagram_roundtrip_and_stats() {
        let mut t = MemoryTransport::new(8, 1200);
        t.send_datagram(Channel::Video, hdr(1), vec![0u8; 100]).unwrap();
        let f = t.recv().expect("frame");
        assert_eq!(f.channel, Channel::Video);
        assert_eq!(f.payload.len(), 100);
        assert_eq!(t.stats().sent_datagrams, 1);
        assert_eq!(t.stats().recv_frames, 1);
        assert_eq!(t.stats().bytes_sent, 100);
    }

    #[test]
    fn datagram_drops_oldest_when_full() {
        let mut t = MemoryTransport::new(2, 1200);
        for seq in 0..4u16 {
            t.send_datagram(Channel::Video, hdr(seq), vec![seq as u8]).unwrap();
        }
        assert_eq!(t.stats().dropped_datagrams, 2);
        assert_eq!(t.recv().unwrap().header.unwrap().seq, 2);
        assert_eq!(t.recv().unwrap().header.unwrap().seq, 3);
        assert!(t.recv().is_none());
    }

    #[test]
    fn reliable_never_drops() {
        let mut t = MemoryTransport::new(2, 1200);
        t.send_reliable(Channel::Control, vec![1]).unwrap();
        t.send_reliable(Channel::Control, vec![2]).unwrap();
        assert_eq!(t.send_reliable(Channel::Control, vec![3]), Err(TransportError::Backpressure));
        assert_eq!(t.stats().dropped_datagrams, 0);
    }

    #[test]
    fn reliable_is_served_before_video() {
        let mut t = MemoryTransport::new(4, 1200);
        t.send_datagram(Channel::Video, hdr(7), vec![7]).unwrap();
        t.send_reliable(Channel::Control, vec![9]).unwrap();
        assert_eq!(t.recv().unwrap().channel, Channel::Control);
    }

    #[test]
    fn rejects_oversized_frames() {
        let mut t = MemoryTransport::new(4, 16);
        assert_eq!(t.send_datagram(Channel::Video, hdr(1), vec![0u8; 32]), Err(TransportError::TooLarge));
        assert_eq!(t.send_reliable(Channel::Data, vec![0u8; 32]), Err(TransportError::TooLarge));
    }

    #[test]
    fn wrong_channel_kind_rejected() {
        let mut t = MemoryTransport::new(4, 1200);
        assert_eq!(t.send_datagram(Channel::Control, hdr(1), vec![0]), Err(TransportError::Backpressure));
        assert_eq!(t.send_reliable(Channel::Video, vec![0]), Err(TransportError::Backpressure));
    }

    #[test]
    fn closed_transport_rejects() {
        let mut t = MemoryTransport::new(4, 1200);
        t.close();
        assert_eq!(t.send_datagram(Channel::Video, hdr(1), vec![0]), Err(TransportError::Closed));
        assert_eq!(t.send_reliable(Channel::Data, vec![0]), Err(TransportError::Closed));
        assert!(t.recv().is_none());
    }
}
