//! 文件会话通道消息（ER-711a）。
//!
//! 只定义有线格式与流控状态机；发送入口由 UI/FFI 显式触发（默认关闭）。
//! 同一时刻只有一个传输（transfer_id 4 字节）。

use crate::file_transfer::{Chunk, FileReceiver, FileSender, TransferError};

pub const MSG_FILE_META: u8 = 0x40;
pub const MSG_FILE_CHUNK: u8 = 0x41;
pub const MSG_FILE_ACK: u8 = 0x42;
pub const MAX_FILE_NAME: usize = 255;
pub const MAX_CHUNK_BYTES: usize = 64 * 1024;

fn put_u32(out: &mut Vec<u8>, v: u32) {
    out.extend_from_slice(&v.to_be_bytes());
}

fn take_u32(buf: &[u8], off: usize) -> Option<u32> {
    let b = buf.get(off..off + 4)?;
    Some(u32::from_be_bytes([b[0], b[1], b[2], b[3]]))
}

/// 发送侧：一次只允许一个未 ACK 块，形成天然背压。
pub struct StreamSender {
    id: u32,
    file: FileSender,
    next: u32,
    in_flight: Option<u32>,
    last_msg: Option<Vec<u8>>,
}

impl StreamSender {
    pub fn new(
        id: u32,
        name: &str,
        data: Vec<u8>,
        chunk_size: usize,
    ) -> Result<Self, TransferError> {
        let file = FileSender::new(name, data, chunk_size)?;
        Ok(Self {
            id,
            file,
            next: 0,
            in_flight: None,
            last_msg: None,
        })
    }

    pub fn id(&self) -> u32 {
        self.id
    }

    pub fn meta(&self) -> Vec<u8> {
        let name = self.file.name().as_bytes();
        let name = &name[..name.len().min(MAX_FILE_NAME)];
        let mut out = vec![MSG_FILE_META];
        put_u32(&mut out, self.id);
        put_u32(&mut out, self.file.total_chunks());
        out.extend_from_slice(&(name.len() as u16).to_be_bytes());
        out.extend_from_slice(name);
        out.extend_from_slice(self.file.file_hash().as_bytes());
        out
    }

    /// 下一块；已有未 ACK 块时返回 None（背压）。
    pub fn next_chunk(&mut self) -> Option<Vec<u8>> {
        if self.in_flight.is_some() || self.next >= self.file.total_chunks() {
            return None;
        }
        let chunk = self.file.chunk(self.next).ok()?;
        let mut out = vec![MSG_FILE_CHUNK];
        put_u32(&mut out, self.id);
        put_u32(&mut out, chunk.index);
        put_u32(&mut out, chunk.data.len() as u32);
        out.extend_from_slice(&chunk.data);
        out.extend_from_slice(chunk.hash.as_bytes());
        self.in_flight = Some(chunk.index);
        self.next += 1;
        self.last_msg = Some(out.clone());
        Some(out)
    }

    pub fn ack(&mut self, index: u32) -> bool {
        if self.in_flight == Some(index) {
            self.in_flight = None;
            self.last_msg = None;
            return true;
        }
        false
    }

    /// ACK 超时：重发最后未确认块（接收端按 index 幂等）。
    pub fn retransmit(&self) -> Option<Vec<u8>> {
        if self.in_flight.is_some() {
            self.last_msg.clone()
        } else {
            None
        }
    }

    pub fn done(&self) -> bool {
        self.next >= self.file.total_chunks() && self.in_flight.is_none()
    }
}

/// 接收侧：解析 meta/chunk，按块校验，缺口补齐前不完成。
pub struct StreamReceiver {
    id: u32,
    receiver: FileReceiver,
    name: String,
}

impl StreamReceiver {
    pub fn from_meta(buf: &[u8]) -> Option<Self> {
        if buf.first() != Some(&MSG_FILE_META) || buf.len() < 1 + 4 + 4 + 2 + 64 {
            return None;
        }
        let id = take_u32(buf, 1)?;
        let total = take_u32(buf, 5)?;
        let name_len = u16::from_be_bytes([buf[9], buf[10]]) as usize;
        if name_len > MAX_FILE_NAME || buf.len() < 11 + name_len + 64 {
            return None;
        }
        let name = std::str::from_utf8(&buf[11..11 + name_len])
            .ok()?
            .to_string();
        let hash = std::str::from_utf8(&buf[11 + name_len..11 + name_len + 64])
            .ok()?
            .to_string();
        Some(Self {
            id,
            receiver: FileReceiver::new(total, hash),
            name,
        })
    }

    pub fn id(&self) -> u32 {
        self.id
    }

    pub fn name(&self) -> &str {
        &self.name
    }

    /// 接收一块，返回 (ack_index, 完成文件)。
    pub fn on_chunk(&mut self, buf: &[u8]) -> Result<(u32, Option<Vec<u8>>), TransferError> {
        if buf.first() != Some(&MSG_FILE_CHUNK) || buf.len() < 13 + 64 {
            return Err(TransferError::OutOfRange);
        }
        let id = take_u32(buf, 1).ok_or(TransferError::OutOfRange)?;
        if id != self.id {
            return Err(TransferError::OutOfRange);
        }
        let index = take_u32(buf, 5).ok_or(TransferError::OutOfRange)?;
        let len = take_u32(buf, 9).ok_or(TransferError::OutOfRange)? as usize;
        if len > MAX_CHUNK_BYTES || buf.len() < 13 + len + 64 {
            return Err(TransferError::OutOfRange);
        }
        let data = buf[13..13 + len].to_vec();
        let hash = std::str::from_utf8(&buf[13 + len..13 + len + 64])
            .map_err(|_| TransferError::OutOfRange)?
            .to_string();
        self.receiver.accept(Chunk { index, data, hash })?;
        if self.receiver.missing().is_empty() {
            // finish 需要所有权；用 missing 判断后无法再 finish。改为每次克隆不现实。
            // 这里返回 ack，完成由 finish_receiver 在流结束时一次性校验。
        }
        Ok((index, None))
    }

    pub fn ack(index: u32, id: u32) -> Vec<u8> {
        let mut out = vec![MSG_FILE_ACK];
        put_u32(&mut out, id);
        put_u32(&mut out, index);
        out
    }

    pub fn finish(self) -> Result<Vec<u8>, TransferError> {
        self.receiver.finish()
    }

    pub fn missing(&self) -> Vec<u32> {
        self.receiver.missing()
    }
}
#[cfg(test)]
mod stream_tests {
    use super::*;

    fn sample(n: usize) -> Vec<u8> {
        (0..n).map(|i| (i % 251) as u8).collect()
    }

    #[test]
    fn full_transfer_with_ack_backpressure() {
        let data = sample(200_000);
        let mut tx = StreamSender::new(7, "a.bin", data.clone(), 64 * 1024).unwrap();
        let mut rx = StreamReceiver::from_meta(&tx.meta()).expect("meta");
        assert_eq!(rx.id(), 7);
        assert_eq!(rx.name(), "a.bin");

        let mut guard = 0;
        while let Some(msg) = tx.next_chunk() {
            guard += 1;
            assert!(guard < 100, "chunk loop runaway");
            let (index, _) = rx.on_chunk(&msg).unwrap();
            assert!(tx.ack(index));
        }
        assert!(tx.done());
        assert_eq!(rx.finish().unwrap(), data);
    }

    #[test]
    fn no_second_chunk_until_ack() {
        let mut tx = StreamSender::new(1, "a.bin", sample(1000), 256).unwrap();
        let first = tx.next_chunk().unwrap();
        assert!(tx.next_chunk().is_none(), "must wait for ACK");
        let index = take_u32(&first, 5).unwrap();
        assert!(tx.ack(index));
        assert!(tx.next_chunk().is_some());
    }

    #[test]
    fn corrupt_chunk_rejected() {
        let mut tx = StreamSender::new(2, "a.bin", sample(600), 256).unwrap();
        let mut rx = StreamReceiver::from_meta(&tx.meta()).unwrap();
        let mut msg = tx.next_chunk().unwrap();
        let idx = take_u32(&msg, 5).unwrap();
        msg[13] ^= 0xff;
        let err = rx.on_chunk(&msg).unwrap_err();
        assert!(matches!(err, TransferError::ChunkCorrupt(i) if i == idx));
    }

    #[test]
    fn retransmit_until_ack() {
        let mut tx = StreamSender::new(9, "a.bin", sample(1000), 256).unwrap();
        let first = tx.next_chunk().unwrap();
        assert_eq!(tx.retransmit().unwrap(), first);
        let index = take_u32(&first, 5).unwrap();
        assert!(tx.ack(index));
        assert!(tx.retransmit().is_none());
    }

    #[test]
    fn duplicate_chunk_is_idempotent() {
        let mut tx = StreamSender::new(10, "a.bin", sample(1000), 256).unwrap();
        let mut rx = StreamReceiver::from_meta(&tx.meta()).unwrap();
        let msg = tx.next_chunk().unwrap();
        let (idx, _) = rx.on_chunk(&msg).unwrap();
        let before = rx.missing();
        let (idx2, _) = rx.on_chunk(&msg).unwrap();
        assert_eq!(idx, idx2);
        assert_eq!(
            before,
            rx.missing(),
            "duplicate must not change missing set"
        );
    }

    #[test]
    fn wrong_transfer_id_rejected() {
        let tx = StreamSender::new(3, "a.bin", sample(10), 256).unwrap();
        let mut rx = StreamReceiver::from_meta(&tx.meta()).unwrap();
        let mut msg = vec![MSG_FILE_CHUNK];
        put_u32(&mut msg, 99);
        put_u32(&mut msg, 0);
        put_u32(&mut msg, 0);
        assert!(rx.on_chunk(&msg).is_err());
    }
}
