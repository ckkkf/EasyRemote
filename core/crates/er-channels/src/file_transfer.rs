//! 文件传输（PRD §5.4）：分块、校验、断点续传。
//!
//! 设计要点：
//!   - 固定块大小，块序号即偏移，续传只需对端告知已完成块数
//!   - 每块与整文件双重哈希，任何一块坏了都能定位到具体块重传
//!   - 接收端按序写入，乱序块先入缓冲；缺口补齐前不落盘

use sha2::{Digest, Sha256};

/// 默认块大小（64 KiB）。与 QUIC 流大小无关，纯粹是文件层的分片单位。
pub const DEFAULT_CHUNK: usize = 64 * 1024;

/// 传输错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TransferError {
    /// 块序号越界
    OutOfRange,
    /// 块哈希不匹配
    ChunkCorrupt(u32),
    /// 整文件哈希不匹配
    FileCorrupt,
    /// 块缺口未补齐就试图完成
    Incomplete,
    /// 超过大小上限
    TooLarge,
}

impl std::fmt::Display for TransferError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TransferError::OutOfRange => write!(f, "chunk out of range"),
            TransferError::ChunkCorrupt(i) => write!(f, "chunk {i} corrupt"),
            TransferError::FileCorrupt => write!(f, "file hash mismatch"),
            TransferError::Incomplete => write!(f, "transfer incomplete"),
            TransferError::TooLarge => write!(f, "file too large"),
        }
    }
}

impl std::error::Error for TransferError {}

pub fn sha256_hex(data: &[u8]) -> String {
    let mut h = Sha256::new();
    h.update(data);
    hex(&h.finalize())
}

fn hex(bytes: &[u8]) -> String {
    const T: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(T[(b >> 4) as usize] as char);
        s.push(T[(b & 0x0f) as usize] as char);
    }
    s
}

/// 一块数据。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Chunk {
    pub index: u32,
    pub data: Vec<u8>,
    /// 该块的 SHA-256（十六进制）
    pub hash: String,
}

/// 发送侧：把文件切成块。
#[derive(Debug, Clone)]
pub struct FileSender {
    name: String,
    data: Vec<u8>,
    chunk_size: usize,
    total_chunks: u32,
    file_hash: String,
}

impl FileSender {
    pub fn new(
        name: impl Into<String>,
        data: Vec<u8>,
        chunk_size: usize,
    ) -> Result<Self, TransferError> {
        if data.len() > 4 * 1024 * 1024 * 1024 {
            return Err(TransferError::TooLarge);
        }
        let cs = if chunk_size == 0 {
            DEFAULT_CHUNK
        } else {
            chunk_size
        };
        let total = data.len().div_ceil(cs).max(1) as u32;
        let file_hash = sha256_hex(&data);
        Ok(Self {
            name: name.into(),
            data,
            chunk_size: cs,
            total_chunks: total,
            file_hash,
        })
    }

    pub fn name(&self) -> &str {
        &self.name
    }
    pub fn total_chunks(&self) -> u32 {
        self.total_chunks
    }
    pub fn file_hash(&self) -> &str {
        &self.file_hash
    }
    pub fn size(&self) -> usize {
        self.data.len()
    }

    /// 取指定块。
    pub fn chunk(&self, index: u32) -> Result<Chunk, TransferError> {
        if index >= self.total_chunks {
            return Err(TransferError::OutOfRange);
        }
        let start = index as usize * self.chunk_size;
        let end = (start + self.chunk_size).min(self.data.len());
        let data = self.data[start..end].to_vec();
        let hash = sha256_hex(&data);
        Ok(Chunk { index, data, hash })
    }

    /// 断点续传：从 from_index 开始取剩余块。
    pub fn chunks_from(&self, from_index: u32) -> Result<Vec<Chunk>, TransferError> {
        if from_index > self.total_chunks {
            return Err(TransferError::OutOfRange);
        }
        (from_index..self.total_chunks)
            .map(|i| self.chunk(i))
            .collect()
    }
}

/// 接收侧：按块落盘，校验完整性。
#[derive(Debug)]
pub struct FileReceiver {
    total_chunks: u32,
    expected_file_hash: String,
    chunks: Vec<Option<Vec<u8>>>,
    received: u32,
}

impl FileReceiver {
    pub fn new(total_chunks: u32, expected_file_hash: impl Into<String>) -> Self {
        Self {
            total_chunks,
            expected_file_hash: expected_file_hash.into(),
            chunks: vec![None; total_chunks as usize],
            received: 0,
        }
    }

    /// 接收一块并校验。重复块幂等。
    pub fn accept(&mut self, chunk: Chunk) -> Result<(), TransferError> {
        if chunk.index >= self.total_chunks {
            return Err(TransferError::OutOfRange);
        }
        let idx = chunk.index as usize;
        if self.chunks[idx].is_some() {
            return Ok(());
        }
        if sha256_hex(&chunk.data) != chunk.hash {
            return Err(TransferError::ChunkCorrupt(chunk.index));
        }
        self.chunks[idx] = Some(chunk.data);
        self.received += 1;
        Ok(())
    }

    /// 已完成的连续前缀长度——续传时对端从这里继续即可。
    pub fn contiguous_prefix(&self) -> u32 {
        let mut n = 0u32;
        for c in &self.chunks {
            if c.is_some() {
                n += 1;
            } else {
                break;
            }
        }
        n
    }

    /// 还缺哪些块。
    pub fn missing(&self) -> Vec<u32> {
        self.chunks
            .iter()
            .enumerate()
            .filter(|(_, c)| c.is_none())
            .map(|(i, _)| i as u32)
            .collect()
    }

    /// 汇总成完整文件，并校验整文件哈希。
    pub fn finish(self) -> Result<Vec<u8>, TransferError> {
        if self.missing().iter().any(|_| true) && self.received != self.total_chunks {
            return Err(TransferError::Incomplete);
        }
        let mut out = Vec::new();
        for c in self.chunks {
            out.extend_from_slice(&c.ok_or(TransferError::Incomplete)?);
        }
        if sha256_hex(&out) != self.expected_file_hash {
            return Err(TransferError::FileCorrupt);
        }
        Ok(out)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample(n: usize) -> Vec<u8> {
        (0..n).map(|i| (i % 251) as u8).collect()
    }

    #[test]
    fn chunk_count_and_boundaries() {
        let s = FileSender::new("a.bin", sample(1000), 256).unwrap();
        assert_eq!(s.total_chunks(), 4); // 256*3 + 232
        assert_eq!(s.chunk(3).unwrap().data.len(), 232);
        assert_eq!(s.chunk(4).unwrap_err(), TransferError::OutOfRange);
    }

    #[test]
    fn full_transfer_roundtrip() {
        let data = sample(300_000);
        let s = FileSender::new("big.bin", data.clone(), DEFAULT_CHUNK).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), s.file_hash().to_string());
        for i in 0..s.total_chunks() {
            r.accept(s.chunk(i).unwrap()).unwrap();
        }
        assert_eq!(r.finish().unwrap(), data);
    }

    #[test]
    fn corrupt_chunk_detected_with_index() {
        let s = FileSender::new("a.bin", sample(600), 256).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), s.file_hash().to_string());
        let mut bad = s.chunk(1).unwrap();
        bad.data[0] ^= 0xff; // 破坏数据但保留原 hash
        assert_eq!(r.accept(bad), Err(TransferError::ChunkCorrupt(1)));
    }

    #[test]
    fn resume_from_contiguous_prefix() {
        let s = FileSender::new("a.bin", sample(1000), 256).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), s.file_hash().to_string());
        // 先收 0、1
        r.accept(s.chunk(0).unwrap()).unwrap();
        r.accept(s.chunk(1).unwrap()).unwrap();
        assert_eq!(r.contiguous_prefix(), 2);

        // 乱序收到 3，前缀仍是 2
        r.accept(s.chunk(3).unwrap()).unwrap();
        assert_eq!(r.contiguous_prefix(), 2);
        assert_eq!(r.missing(), vec![2]);

        for chunk in s.chunks_from(2).unwrap() {
            let _ = r.accept(chunk);
        }
        assert_eq!(r.finish().unwrap(), sample(1000));
    }

    #[test]
    fn duplicate_chunk_is_idempotent() {
        let s = FileSender::new("a.bin", sample(300), 256).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), s.file_hash().to_string());
        let c = s.chunk(0).unwrap();
        r.accept(c.clone()).unwrap();
        r.accept(c).unwrap();
        assert_eq!(r.contiguous_prefix(), 1);
    }

    #[test]
    fn incomplete_finish_rejected() {
        let s = FileSender::new("a.bin", sample(600), 256).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), s.file_hash().to_string());
        r.accept(s.chunk(0).unwrap()).unwrap();
        assert_eq!(r.finish().unwrap_err(), TransferError::Incomplete);
    }

    #[test]
    fn wrong_file_hash_rejected() {
        let s = FileSender::new("a.bin", sample(256), 256).unwrap();
        let mut r = FileReceiver::new(s.total_chunks(), "deadbeef");
        r.accept(s.chunk(0).unwrap()).unwrap();
        assert_eq!(r.finish().unwrap_err(), TransferError::FileCorrupt);
    }
}
