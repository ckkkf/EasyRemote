//! 中继客户端：注册 + 收发。
//!
//! 一个设备一个 socket。注册成功后中继会把对端发来的数据原样投递到本 socket。

use std::collections::VecDeque;
use std::net::{SocketAddr, UdpSocket};
use std::sync::Mutex;
use std::time::Duration;

use crate::ticket::{issue, Ticket};
pub use er_transport::p2p::P2PConfig;
use er_transport::p2p::{
    decode_candidates, encode_candidates, punch_candidate, Candidate, CandidateKind,
};

use crate::wire::{
    encode_frame, encode_register, fragment, parse_frame, Reassembler, FRAG_HEADER, FRAME_DATA,
    FRAME_REGISTER, FRAME_REGISTER_ACK, MAX_DATAGRAM, REGISTER_ACK_MAGIC,
};
pub use er_transport::p2p::{Candidate as P2PCandidate, P2PConfig as RelayP2PConfig};

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
    /// 注册等待 ACK 期间先到达的数据帧必须暂存，不能丢。
    pending: Mutex<VecDeque<Vec<u8>>>,
    /// P2P 专用 UDP socket（与 Relay socket 分离，避免 connected 过滤）。
    p2p_socket: Option<UdpSocket>,
    peer_candidates: Vec<Candidate>,
    p2p_peer: Option<SocketAddr>,
    link: LinkKind,
    /// 单个分片的最大载荷（留出分片头与帧头的余量）
    max_payload: usize,
}

/// 当前实际链路。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LinkKind {
    Relay,
    P2P,
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
            pending: Mutex::new(VecDeque::new()),
            p2p_socket: None,
            peer_candidates: Vec::new(),
            p2p_peer: None,
            link: LinkKind::Relay,
            // 1200B 以内避免 IP 分片；大帧由分片层切成多包。
            max_payload: 1200,
        })
    }

    pub fn local_addr(&self) -> SocketAddr {
        self.sock.local_addr().expect("bound socket")
    }

    pub fn relay_addr(&self) -> SocketAddr {
        self.relay
    }

    /// 用票据注册，并等待中继的 REGISTER_ACK。
    ///
    /// UDP 发送成功不代表中继收下了：丢包/中继重启/票据被拒都可能没有 ACK。
    /// 没有 ACK 一律视为未注册，最多重试 3 次。
    pub fn register(&self, ticket: &str) -> Result<(), ClientError> {
        let body = encode_register(&self.device_id, ticket);
        let frame = encode_frame(FRAME_REGISTER, &self.session_id, &body);
        for _ in 0..3 {
            self.sock.send(&frame)?;
            if self.wait_register_ack(Duration::from_millis(500))? {
                self.sock
                    .set_read_timeout(Some(Duration::from_millis(500)))?;
                return Ok(());
            }
        }
        self.sock
            .set_read_timeout(Some(Duration::from_millis(500)))?;
        Err(ClientError::NotRegistered)
    }

    /// 等待 REGISTER_ACK；超时返回 false。
    fn wait_register_ack(&self, timeout: Duration) -> Result<bool, ClientError> {
        let deadline = std::time::Instant::now() + timeout;
        let mut buf = vec![0u8; MAX_DATAGRAM];
        loop {
            let now = std::time::Instant::now();
            if now >= deadline {
                return Ok(false);
            }
            self.sock
                .set_read_timeout(Some(deadline.saturating_duration_since(now)))?;
            match self.sock.recv(&mut buf) {
                Ok(n) => {
                    if let Ok((kind, sid, body)) = parse_frame(&buf[..n]) {
                        if kind == FRAME_REGISTER_ACK
                            && sid == self.session_id
                            && body.starts_with(REGISTER_ACK_MAGIC)
                        {
                            return Ok(true);
                        }
                    }
                    // 对端可能已经开始握手：先缓存，register 返回后按顺序消费。
                    if let Ok(mut q) = self.pending.lock() {
                        q.push_back(buf[..n].to_vec());
                    }
                }
                Err(e)
                    if e.kind() == std::io::ErrorKind::WouldBlock
                        || e.kind() == std::io::ErrorKind::TimedOut =>
                {
                    return Ok(false)
                }
                Err(e) => return Err(ClientError::Io(e)),
            }
        }
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
            match self.register(ticket) {
                Ok(()) => return Ok(attempt),
                Err(e) if start.elapsed() < max_total && policy.should_retry(attempt) => {
                    let _ = e;
                    std::thread::sleep(policy.delay(attempt));
                    attempt += 1;
                }
                Err(e) => return Err(e),
            }
        }
    }

    /// 便捷方法：用本地 secret 自签票据并注册。
    pub fn register_with_secret(
        &self,
        secret: &[u8],
        peer_device: &str,
        ttl_secs: i64,
    ) -> Result<(), ClientError> {
        let exp = now_unix() + ttl_secs;
        let tk = issue(secret, &self.session_id, &self.device_id, peer_device, exp);
        self.register(&tk)
    }

    /// 当前链路类型（Relay/P2P）。
    pub fn link_kind(&self) -> LinkKind {
        self.link
    }

    /// 准备 P2P：创建独立 UDP socket，并把 host 候选通过 Relay 发给对端。
    pub fn prepare_p2p(&mut self, cfg: &P2PConfig) -> Result<(), ClientError> {
        if self.p2p_socket.is_none() {
            let sock = UdpSocket::bind("0.0.0.0:0")?;
            sock.set_read_timeout(Some(Duration::from_millis(200)))?;
            self.p2p_socket = Some(sock);
        }
        let port = self.p2p_socket.as_ref().unwrap().local_addr()?.port();
        // 0.0.0.0 不是可连接地址：用一条到 Relay 的路由 socket 取本机出口 IP。
        let route = UdpSocket::bind("0.0.0.0:0")?;
        route.connect(self.relay)?;
        let ip = route.local_addr()?.ip();
        let cands = vec![Candidate {
            kind: CandidateKind::Host,
            addr: SocketAddr::new(ip, port),
        }];
        let msg = encode_candidates(cfg, &cands);
        self.send(&msg)?;
        Ok(())
    }

    /// 等待对端候选（通过 Relay 数据通道）。
    pub fn await_candidates(
        &mut self,
        cfg: &P2PConfig,
        timeout: Duration,
    ) -> Result<Vec<Candidate>, ClientError> {
        let deadline = std::time::Instant::now() + timeout;
        while std::time::Instant::now() < deadline {
            let left = deadline.saturating_duration_since(std::time::Instant::now());
            match self.recv_timeout(left)? {
                Some(msg) => {
                    if let Some(cands) = decode_candidates(&msg, cfg) {
                        self.peer_candidates = cands.clone();
                        return Ok(cands);
                    }
                }
                None => break,
            }
        }
        Ok(Vec::new())
    }

    /// 独立进程的一站式协商：发候选 → 收候选 → 打洞 → ready → 切换。
    /// 双方按同样顺序调用即可；失败保持 Relay。
    pub fn negotiate_p2p(&mut self, cfg: &P2PConfig) -> bool {
        if self.prepare_p2p(cfg).is_err() {
            return false;
        }
        match self.await_candidates(cfg, Duration::from_secs(2)) {
            Ok(cands) if !cands.is_empty() => self.set_peer_candidates(cands),
            _ => return false,
        }
        let local_ok = self.punch(cfg);
        if self.send_ready(cfg, local_ok).is_err() {
            return false;
        }
        let peer_ok = self
            .await_ready(cfg, Duration::from_secs(2))
            .unwrap_or(false);
        self.activate_p2p(local_ok, peer_ok);
        self.link == LinkKind::P2P
    }

    pub fn set_peer_candidates(&mut self, cands: Vec<Candidate>) {
        self.peer_candidates = cands;
    }

    /// 尝试所有候选打洞；成功则记录 P2P socket/peer（尚未切换链路）。
    pub fn punch(&mut self, cfg: &P2PConfig) -> bool {
        let Some(sock) = self.p2p_socket.as_ref() else {
            return false;
        };
        let cands = self.peer_candidates.clone();
        for c in cands {
            if c.kind == CandidateKind::Relay {
                continue;
            }
            if punch_candidate(sock, c.addr, cfg) && sock.connect(c.addr).is_ok() {
                self.p2p_peer = Some(c.addr);
                return true;
            }
        }
        false
    }

    /// 通过 Relay 发送 ready（切换前的两阶段确认）。
    pub fn send_ready(&mut self, cfg: &P2PConfig, ok: bool) -> Result<(), ClientError> {
        let mut msg = Vec::with_capacity(8 + 16 + cfg.session_id.len() + 1);
        msg.extend_from_slice(b"ERLINK2");
        msg.extend_from_slice(&cfg.token);
        msg.extend_from_slice(&(cfg.session_id.len() as u16).to_be_bytes());
        msg.extend_from_slice(cfg.session_id.as_bytes());
        msg.push(if ok { 1 } else { 0 });
        // 必须走分片层：Relay 只转发 body，裸帧会被对端当成 fragment 解析。
        self.send(&msg)?;
        Ok(())
    }

    /// 等待对端 ready；返回对端是否 P2P 成功。
    pub fn await_ready(&mut self, cfg: &P2PConfig, timeout: Duration) -> Result<bool, ClientError> {
        let deadline = std::time::Instant::now() + timeout;
        while std::time::Instant::now() < deadline {
            let left = deadline.saturating_duration_since(std::time::Instant::now());
            if let Some(msg) = self.recv_timeout(left)? {
                if msg.len() >= 8 + 16 + 2 && &msg[..7] == b"ERLINK2" && msg[7..23] == cfg.token {
                    let sid_len = u16::from_be_bytes([msg[23], msg[24]]) as usize;
                    let off = 25 + sid_len;
                    if msg.len() > off && &msg[25..off] == cfg.session_id.as_bytes() {
                        return Ok(msg[off] == 1);
                    }
                }
            } else {
                break;
            }
        }
        Ok(false)
    }

    /// 双方都成功才切 P2P；否则清理并保持 Relay。
    pub fn activate_p2p(&mut self, local_ok: bool, peer_ok: bool) {
        if local_ok && peer_ok && self.p2p_peer.is_some() {
            self.link = LinkKind::P2P;
        } else {
            self.link = LinkKind::Relay;
            self.p2p_peer = None;
        }
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
            if self.link == LinkKind::P2P {
                if let Some(sock) = self.p2p_socket.as_ref() {
                    if sock.send(&f).is_ok() {
                        continue;
                    }
                    // P2P 发失败立即回退 Relay，避免静默丢包。
                    self.link = LinkKind::Relay;
                }
            }
            let frame = encode_frame(FRAME_DATA, &self.session_id, &f);
            self.sock.send(&frame)?;
        }
        self.msg_seq = self.msg_seq.wrapping_add(1);
        Ok(n)
    }

    /// 收一个完整消息（阻塞至超时）。返回 None 表示本次没收到完整消息。
    pub fn recv(&mut self) -> Result<Option<Vec<u8>>, ClientError> {
        if let Ok(mut q) = self.pending.lock() {
            if let Some(raw) = q.pop_front() {
                return Ok(self.reassembler.push(&raw));
            }
        }
        if self.link == LinkKind::P2P {
            if let Some(sock) = self.p2p_socket.as_ref() {
                let _ = sock.set_read_timeout(Some(Duration::from_millis(1)));
                let mut p2p_buf = vec![0u8; MAX_DATAGRAM];
                if let Ok(n) = sock.recv(&mut p2p_buf) {
                    return Ok(self.reassembler.push(&p2p_buf[..n]));
                }
            }
        }
        let mut buf = vec![0u8; MAX_DATAGRAM];
        match self.sock.recv(&mut buf) {
            Ok(n) => {
                // 迟到的重复 REGISTER_ACK 不是数据分片，必须丢弃，否则会污染重组。
                if buf[..n].starts_with(REGISTER_ACK_MAGIC) {
                    return Ok(None);
                }
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
/// 会话内 P2P probe token（真实生产应替换为会话密钥派生）。
pub fn p2p_token(session_id: &str) -> [u8; 16] {
    let mut out = [0u8; 16];
    for (i, b) in session_id.bytes().enumerate() {
        out[i % 16] ^= b;
    }
    for (i, b) in out.iter_mut().enumerate() {
        *b = b.wrapping_add((i as u8).wrapping_mul(31));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::UdpSocket;
    use std::thread;

    #[test]
    fn register_waits_for_ack() {
        let server = UdpSocket::bind("127.0.0.1:0").expect("bind");
        server
            .set_read_timeout(Some(Duration::from_secs(2)))
            .unwrap();
        let addr = server.local_addr().unwrap();
        let handle = thread::spawn(move || {
            let mut buf = [0u8; MAX_DATAGRAM];
            let (n, from) = server.recv_from(&mut buf).expect("recv register");
            let (kind, sid, _) = parse_frame(&buf[..n]).expect("parse");
            assert_eq!(kind, FRAME_REGISTER);
            let ack = encode_frame(FRAME_REGISTER_ACK, &sid, b"ERACK1dev");
            server.send_to(&ack, from).expect("send ack");
        });
        let client = RelayClient::new(addr, "ses-ack", "dev").expect("client");
        client.register("ticket").expect("register should see ack");
        handle.join().unwrap();
    }

    #[test]
    fn register_without_ack_is_not_registered() {
        let server = UdpSocket::bind("127.0.0.1:0").expect("bind");
        let addr = server.local_addr().unwrap();
        let handle = thread::spawn(move || {
            let mut buf = [0u8; MAX_DATAGRAM];
            let _ = server.recv_from(&mut buf);
        });
        let client = RelayClient::new(addr, "ses-noack", "dev").expect("client");
        assert!(
            client.register("ticket").is_err(),
            "no ack must fail registration"
        );
        handle.join().unwrap();
    }
    #[test]
    fn activate_p2p_requires_both_sides() {
        let server = UdpSocket::bind("127.0.0.1:0").unwrap();
        let addr = server.local_addr().unwrap();
        let mut c = RelayClient::new(addr, "s", "d").unwrap();
        c.p2p_peer = Some("127.0.0.1:1234".parse().unwrap());
        c.activate_p2p(true, false);
        assert_eq!(c.link, LinkKind::Relay);
        c.activate_p2p(false, true);
        assert_eq!(c.link, LinkKind::Relay);
        c.p2p_peer = Some("127.0.0.1:1234".parse().unwrap());
        c.activate_p2p(true, true);
        assert_eq!(c.link, LinkKind::P2P);
    }
    /// 中继重启：先断开，再在同一端口恢复；客户端退避重试后注册成功。
    #[test]
    fn register_recovers_after_relay_restart() {
        let probe = UdpSocket::bind("127.0.0.1:0").unwrap();
        let addr = probe.local_addr().unwrap();
        drop(probe);

        let server = thread::spawn(move || {
            std::thread::sleep(Duration::from_millis(300));
            let sock = UdpSocket::bind(addr).expect("rebind relay port");
            sock.set_read_timeout(Some(Duration::from_secs(5))).unwrap();
            let mut buf = [0u8; MAX_DATAGRAM];
            let (n, from) = sock.recv_from(&mut buf).expect("register");
            let (kind, sid, _) = parse_frame(&buf[..n]).expect("parse");
            assert_eq!(kind, FRAME_REGISTER);
            let ack = encode_frame(FRAME_REGISTER_ACK, &sid, b"ERACK1dev");
            sock.send_to(&ack, from).expect("ack");
        });

        let client = RelayClient::new(addr, "ses-restart", "dev").expect("client");
        let attempts = client
            .register_with_retry(
                "ticket",
                crate::backoff::BackoffPolicy::fast(),
                Duration::from_secs(5),
            )
            .expect("should recover after relay restart");
        assert!(attempts <= 2, "unexpected attempts: {attempts}");
        server.join().unwrap();
    }
}
