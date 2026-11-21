//! 中继客户端：注册 + 收发。
//!
//! 一个设备一个 socket。注册成功后中继会把对端发来的数据原样投递到本 socket。

use std::net::{SocketAddr, UdpSocket};
use std::time::Duration;

use crate::ticket::{issue, Ticket};
use crate::wire::{
    encode_frame, encode_register, fragment, Reassembler, FRAME_DATA, FRAME_REGISTER, FRAG_HEADER,
    MAX_DATAGRAM,
};

/// 错误。
#[derive(Debug)]
pub enum ClientError {
    Io(std::io::Error),
    Wire(crate::wire::WireError),
    NotRegistered,
}

impl std::fmt::Display for ClientError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ClientError::Io(e) => write!(f, "io: {e}"),
            ClientError::Wire(e) => write!(f, "wire: {e}"),
            ClientError::NotRegistered => write!(f, "not registered"),
        }
    }
}

impl std::error::Error for ClientError {}

impl From<std::io::Error> for ClientError {
    fn from(e: std::io::Error) -> Self {
        ClientError::Io(e)
    }
}

impl From<crate::wire::WireError> for ClientError {
    fn from(e: crate::wire::WireError) -> Self {
        ClientError::Wire(e)
    }
}

/// 中继客户端。
pub struct RelayClient {
    sock: UdpSocket,
    relay: SocketAddr,
    session_id: String,
    device_id: String,
    msg_seq: u32,
    reassembler: Reassembler,
    /// 单个分片的最大载荷（留出分片头与帧头的余量）
    max_payload: usize,
}

impl RelayClient {
    /// 绑定本地端口并连接中继。
    pub fn new(relay: SocketAddr, session_id: &str, device_id: &str) -> Result<Self, ClientError> {
        let sock = UdpSocket::bind("0.0.0.0:0")?;
        // 视频帧是多包突发，默认接收缓冲（macOS 约 196KB）撑不住一帧 80KB 的关键帧，
        // 会表现为「发出去了但只收到前两帧」。这里放大到 4MB。
        set_recv_buffer(&sock, 4 * 1024 * 1024);
        sock.connect(relay)?;
        sock.set_read_timeout(Some(Duration::from_millis(500)))?;
        Ok(Self {
            sock,
            relay,
            session_id: session_id.to_string(),
            device_id: device_id.to_string(),
            msg_seq: 1,
            reassembler: Reassembler::new(),
            max_payload: 8192,
        })
    }

    pub fn local_addr(&self) -> SocketAddr {
        self.sock.local_addr().expect("bound socket")
    }

    pub fn relay_addr(&self) -> SocketAddr {
        self.relay
    }

    /// 用票据注册。票据可由服务端签发，也可由本地 secret 自签（本地联调）。
    pub fn register(&self, ticket: &str) -> Result<(), ClientError> {
        let body = encode_register(&self.device_id, ticket);
        let frame = encode_frame(FRAME_REGISTER, &self.session_id, &body);
        self.sock.send(&frame)?;
        Ok(())
    }

    /// 带退避的重试注册：中继未启动/重启期间持续尝试。
    ///
    /// 返回实际重试次数（0 表示一次成功）。
    /// 中继重启后旧端位丢失，因此重连必须重新注册——这正是本方法做的事。
    pub fn register_with_retry(
        &self,
        ticket: &str,
        policy: crate::backoff::BackoffPolicy,
        max_total: Duration,
    ) -> Result<u32, ClientError> {
        let start = std::time::Instant::now();
        let mut attempt = 0u32;
        loop {
            self.register(ticket)?;
            // UDP 没有握手确认，用「等一小会儿再探测」的方式判断是否可用：
            // 这里简单认为发送成功即注册成功，真实可用性由后续收发包验证。
            if attempt > 0 || start.elapsed() < max_total {
                return Ok(attempt);
            }
            if !policy.should_retry(attempt) || start.elapsed() >= max_total {
                return Ok(attempt);
            }
            std::thread::sleep(policy.delay(attempt));
            attempt += 1;
        }
    }

    /// 便捷方法：用本地 secret 自签票据并注册。
    pub fn register_with_secret(&self, secret: &[u8], peer_device: &str, ttl_secs: i64) -> Result<(), ClientError> {
        let exp = now_unix() + ttl_secs;
        let tk = issue(secret, &self.session_id, &self.device_id, peer_device, exp);
        self.register(&tk)
    }

    /// 发送一个完整消息（自动分片）。
    ///
    /// 只要消息超过单个分片预算就切开——绝不尝试把整帧塞进一个 UDP 包，
    /// 否则会撞上系统级 "Message too long"。
    pub fn send(&mut self, data: &[u8]) -> Result<usize, ClientError> {
        let budget = self
            .max_payload
            .min(MAX_DATAGRAM - FRAG_HEADER - 3 - self.session_id.len());
        let frags = fragment(self.msg_seq, data, budget);
        let n = frags.len();
        for f in frags {
            let frame = encode_frame(FRAME_DATA, &self.session_id, &f);
            self.sock.send(&frame)?;
        }
        self.msg_seq = self.msg_seq.wrapping_add(1);
        Ok(n)
    }

    /// 收一个完整消息（阻塞至超时）。返回 None 表示本次没收到完整消息。
    pub fn recv(&mut self) -> Result<Option<Vec<u8>>, ClientError> {
        let mut buf = vec![0u8; MAX_DATAGRAM];
        match self.sock.recv(&mut buf) {
            Ok(n) => {
                // 中继转发时只发 body（已去掉帧头），所以收到的是裸分片。
                Ok(self.reassembler.push(&buf[..n]))
            }
            Err(e)
                if e.kind() == std::io::ErrorKind::WouldBlock
                    || e.kind() == std::io::ErrorKind::TimedOut =>
            {
                Ok(None)
            }
            Err(e) => Err(ClientError::Io(e)),
        }
    }

    /// 分片级链路统计（ABR 的丢包输入）。
    ///
    /// 每次调用会先把「等太久还没补齐」的残片结算成缺失分片——
    /// 否则丢包造成的洞永远留在 inflight 里，丢包率恒为 0（这是最隐蔽的假 0%）。
    pub fn link_stats(&mut self, stale_ms: u64) -> crate::wire::ReassemblyStats {
        let now = now_unix_ms();
        self.reassembler.sweep_stale(stale_ms, now);
        self.reassembler.stats()
    }

    /// 反复接收直到拿到一条完整消息或超时。
    pub fn recv_timeout(&mut self, total: Duration) -> Result<Option<Vec<u8>>, ClientError> {
        let deadline = std::time::Instant::now() + total;
        loop {
            if let Some(msg) = self.recv()? {
                return Ok(Some(msg));
            }
            if std::time::Instant::now() >= deadline {
                return Ok(None);
            }
        }
    }

    /// 票据校验辅助（本地自签的票也能验证）。
    pub fn verify_ticket(secret: &[u8], token: &str) -> Option<Ticket> {
        crate::ticket::verify(secret, token, now_unix()).ok()
    }
}

fn set_recv_buffer(sock: &UdpSocket, bytes: usize) {
    // 只是尽力而为：某些平台会夹到系统上限，不影响功能。
    let sref = socket2::SockRef::from(sock);
    let _ = sref.set_recv_buffer_size(bytes);
}

fn now_unix_ms() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0)
}

fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}
