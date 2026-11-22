//! P2P 候选与打洞（ER-502）。
//!
//! 目标：先尝试 host/STUN 候选直连，3 秒内失败回落 Relay。
//! 安全：probe/ack 必须带会话共享 token，防止任意 UDP 源注入连接。
//! 局限：未实现 ICE full/controlled 角色与 TURN；NAT 对称场景直接回落 Relay。

use std::io;
use std::net::{IpAddr, Ipv4Addr, SocketAddr, UdpSocket};
use std::time::{Duration, Instant};

const PROBE_MAGIC: &[u8; 6] = b"ERP2P1";
const PROBE: u8 = 1;
const ACK: u8 = 2;
const STUN_MAGIC: u32 = 0x2112_A442;
const STUN_BINDING_REQUEST: u16 = 0x0001;
const STUN_BINDING_RESPONSE: u16 = 0x0101;
const STUN_ATTR_XOR_MAPPED: u16 = 0x0020;

/// 候选类型。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CandidateKind {
    Host,
    ServerReflexive,
    Relay,
}

/// 一个候选地址。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Candidate {
    pub kind: CandidateKind,
    pub addr: SocketAddr,
}

/// P2P 配置。
#[derive(Debug, Clone)]
pub struct P2PConfig {
    pub session_id: String,
    pub token: [u8; 16],
    pub timeout: Duration,
}

impl P2PConfig {
    pub fn new(session_id: &str, token: [u8; 16], timeout: Duration) -> Self {
        Self {
            session_id: session_id.to_string(),
            token,
            timeout,
        }
    }
}

/// 链路选择结果。
#[derive(Debug)]
pub enum Link {
    P2P { socket: UdpSocket, peer: SocketAddr },
    Relay,
}

/// 收集本机候选（host）。未配置 STUN 时只有 host 候选。
pub fn gather_host_candidates() -> Vec<Candidate> {
    let mut out = Vec::new();
    let Ok(sock) = UdpSocket::bind("0.0.0.0:0") else {
        return out;
    };
    if sock.connect("8.8.8.8:80").is_err() && sock.connect("127.0.0.1:9").is_err() {
        return out;
    }
    if let Ok(addr) = sock.local_addr() {
        if !addr.ip().is_unspecified() {
            out.push(Candidate {
                kind: CandidateKind::Host,
                addr,
            });
        }
    }
    out
}

/// 构造 STUN Binding Request（RFC5389）。
pub fn stun_binding_request(txid: [u8; 12]) -> [u8; 28] {
    let mut out = [0u8; 28];
    out[0..2].copy_from_slice(&STUN_BINDING_REQUEST.to_be_bytes());
    out[2..4].copy_from_slice(&0u16.to_be_bytes());
    out[4..8].copy_from_slice(&STUN_MAGIC.to_be_bytes());
    out[8..20].copy_from_slice(&txid);
    out
}

/// 查询 STUN 服务器，返回映射地址。
pub fn stun_query(
    sock: &UdpSocket,
    server: SocketAddr,
    timeout: Duration,
) -> io::Result<SocketAddr> {
    let mut txid = [0u8; 12];
    getrandom::getrandom(&mut txid).map_err(|e| io::Error::other(format!("csprng: {e:?}")))?;
    let req = stun_binding_request(txid);
    sock.send_to(&req, server)?;
    sock.set_read_timeout(Some(timeout))?;
    let mut buf = [0u8; 512];
    let deadline = Instant::now() + timeout;
    loop {
        let left = deadline.saturating_duration_since(Instant::now());
        if left.is_zero() {
            return Err(io::Error::new(io::ErrorKind::TimedOut, "stun timeout"));
        }
        sock.set_read_timeout(Some(left))?;
        let (n, from) = sock.recv_from(&mut buf)?;
        if from != server || n < 20 {
            continue;
        }
        if u16::from_be_bytes([buf[0], buf[1]]) != STUN_BINDING_RESPONSE {
            continue;
        }
        let len = u16::from_be_bytes([buf[2], buf[3]]) as usize;
        if n < 20 + len {
            continue;
        }
        let mut off = 20;
        while off + 4 <= 20 + len {
            let typ = u16::from_be_bytes([buf[off], buf[off + 1]]);
            let alen = u16::from_be_bytes([buf[off + 2], buf[off + 3]]) as usize;
            off += 4;
            if off + alen > 20 + len {
                break;
            }
            if typ == STUN_ATTR_XOR_MAPPED && alen >= 8 {
                let family = buf[off + 1];
                if family == 0x01 {
                    let port = u16::from_be_bytes([buf[off + 2], buf[off + 3]])
                        ^ (STUN_MAGIC >> 16) as u16;
                    let xaddr = u32::from_be_bytes([
                        buf[off + 4],
                        buf[off + 5],
                        buf[off + 6],
                        buf[off + 7],
                    ]);
                    let ip = Ipv4Addr::from(xaddr ^ STUN_MAGIC);
                    return Ok(SocketAddr::new(IpAddr::V4(ip), port));
                }
            }
            off += (alen + 3) & !3;
        }
    }
}

fn probe_packet(cfg: &P2PConfig, kind: u8) -> Vec<u8> {
    let mut out = Vec::with_capacity(PROBE_MAGIC.len() + 16 + 1 + cfg.session_id.len());
    out.extend_from_slice(PROBE_MAGIC);
    out.extend_from_slice(&cfg.token);
    out.push(kind);
    out.extend_from_slice(cfg.session_id.as_bytes());
    out
}

fn valid_probe(buf: &[u8], cfg: &P2PConfig) -> Option<u8> {
    if buf.len() < PROBE_MAGIC.len() + 17 {
        return None;
    }
    if &buf[..PROBE_MAGIC.len()] != PROBE_MAGIC {
        return None;
    }
    if buf[6..22] != cfg.token {
        return None;
    }
    let kind = buf[22];
    if &buf[23..] != cfg.session_id.as_bytes() {
        return None;
    }
    Some(kind)
}

/// 对一个候选打洞：发 probe，等 ACK。成功返回 true。
pub fn punch_candidate(sock: &UdpSocket, peer: SocketAddr, cfg: &P2PConfig) -> bool {
    let deadline = Instant::now() + cfg.timeout;
    let probe = probe_packet(cfg, PROBE);
    let ack = probe_packet(cfg, ACK);
    while Instant::now() < deadline {
        if sock.send_to(&probe, peer).is_err() {
            return false;
        }
        let left = deadline.saturating_duration_since(Instant::now());
        let _ = sock.set_read_timeout(Some(left.min(Duration::from_millis(200))));
        let mut buf = [0u8; 512];
        match sock.recv_from(&mut buf) {
            Ok((n, from)) => {
                if from != peer {
                    continue;
                }
                match valid_probe(&buf[..n], cfg) {
                    Some(PROBE) => {
                        let _ = sock.send_to(&ack, peer);
                    }
                    Some(ACK) => return true,
                    _ => {}
                }
            }
            Err(e)
                if e.kind() == io::ErrorKind::WouldBlock || e.kind() == io::ErrorKind::TimedOut => {
            }
            Err(_) => return false,
        }
    }
    false
}

/// 按候选顺序尝试打洞；全部失败回落 Relay。
pub fn select_link(sock: UdpSocket, candidates: &[Candidate], cfg: &P2PConfig) -> Link {
    for c in candidates {
        if c.kind == CandidateKind::Relay {
            continue;
        }
        if punch_candidate(&sock, c.addr, cfg) {
            return Link::P2P {
                socket: sock,
                peer: c.addr,
            };
        }
    }
    Link::Relay
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    fn xor_addr(addr: SocketAddr) -> ([u8; 2], [u8; 4]) {
        let port = addr.port() ^ (STUN_MAGIC >> 16) as u16;
        let SocketAddr::V4(v4) = addr else {
            panic!("ipv4 only");
        };
        let x = u32::from(*v4.ip()) ^ STUN_MAGIC;
        (port.to_be_bytes(), x.to_be_bytes())
    }

    #[test]
    fn stun_query_returns_mapped_address() {
        let server = UdpSocket::bind("127.0.0.1:0").unwrap();
        let server_addr = server.local_addr().unwrap();
        let handle = thread::spawn(move || {
            let mut buf = [0u8; 128];
            let (n, from) = server.recv_from(&mut buf).unwrap();
            let txid = &buf[8..20];
            let (xport, xaddr) = xor_addr(from);
            let mut resp = Vec::new();
            resp.extend_from_slice(&STUN_BINDING_RESPONSE.to_be_bytes());
            resp.extend_from_slice(&12u16.to_be_bytes());
            resp.extend_from_slice(&STUN_MAGIC.to_be_bytes());
            resp.extend_from_slice(txid);
            resp.extend_from_slice(&STUN_ATTR_XOR_MAPPED.to_be_bytes());
            resp.extend_from_slice(&8u16.to_be_bytes());
            resp.push(0);
            resp.push(0x01);
            resp.extend_from_slice(&xport);
            resp.extend_from_slice(&xaddr);
            assert_eq!(n, 28);
            server.send_to(&resp, from).unwrap();
        });
        let client = UdpSocket::bind("127.0.0.1:0").unwrap();
        let mapped = stun_query(&client, server_addr, Duration::from_secs(2)).unwrap();
        assert_eq!(mapped, client.local_addr().unwrap());
        handle.join().unwrap();
    }

    #[test]
    fn punch_loopback_succeeds() {
        let cfg = P2PConfig::new("ses-p2p", [7u8; 16], Duration::from_secs(2));
        let server = UdpSocket::bind("127.0.0.1:0").unwrap();
        let server_addr = server.local_addr().unwrap();
        let server_cfg = cfg.clone();
        let handle = thread::spawn(move || {
            let mut buf = [0u8; 256];
            let deadline = Instant::now() + Duration::from_secs(2);
            while Instant::now() < deadline {
                let _ = server.set_read_timeout(Some(Duration::from_millis(100)));
                if let Ok((n, from)) = server.recv_from(&mut buf) {
                    if valid_probe(&buf[..n], &server_cfg) == Some(PROBE) {
                        let ack = probe_packet(&server_cfg, ACK);
                        let _ = server.send_to(&ack, from);
                        return;
                    }
                }
            }
        });
        let client = UdpSocket::bind("127.0.0.1:0").unwrap();
        assert!(punch_candidate(&client, server_addr, &cfg));
        handle.join().unwrap();
    }

    #[test]
    fn punch_failure_falls_back_to_relay() {
        let cfg = P2PConfig::new("ses-fail", [9u8; 16], Duration::from_millis(150));
        let sock = UdpSocket::bind("127.0.0.1:0").unwrap();
        let dead = UdpSocket::bind("127.0.0.1:0").unwrap();
        let addr = dead.local_addr().unwrap();
        drop(dead);
        let link = select_link(
            sock,
            &[Candidate {
                kind: CandidateKind::Host,
                addr,
            }],
            &cfg,
        );
        assert!(matches!(link, Link::Relay));
    }
}

/// 候选交换消息的魔数/编码上限。
pub const CANDIDATE_MSG_MAGIC: &[u8; 7] = b"ERLINK1";
const MAX_CANDIDATES: usize = 16;

/// 编码候选列表：magic + token(16) + session_id + count + (kind,u32 ip,u16 port)*
pub fn encode_candidates(cfg: &P2PConfig, candidates: &[Candidate]) -> Vec<u8> {
    // 编码：magic+token+sidLen(u16)+sid+count
    let mut buf = Vec::new();
    buf.extend_from_slice(CANDIDATE_MSG_MAGIC);
    buf.extend_from_slice(&cfg.token);
    buf.extend_from_slice(&(cfg.session_id.len() as u16).to_be_bytes());
    buf.extend_from_slice(cfg.session_id.as_bytes());
    buf.push(candidates.len().min(MAX_CANDIDATES) as u8);
    for c in candidates.iter().take(MAX_CANDIDATES) {
        let SocketAddr::V4(v4) = c.addr else {
            continue;
        };
        buf.push(match c.kind {
            CandidateKind::Host => 1,
            CandidateKind::ServerReflexive => 2,
            CandidateKind::Relay => 3,
        });
        buf.extend_from_slice(&u32::from(*v4.ip()).to_be_bytes());
        buf.extend_from_slice(&v4.port().to_be_bytes());
    }
    buf
}

/// 解码候选列表；token/session 不匹配返回 None。
pub fn decode_candidates(buf: &[u8], cfg: &P2PConfig) -> Option<Vec<Candidate>> {
    let fixed = CANDIDATE_MSG_MAGIC.len() + 16 + 2;
    if buf.len() < fixed || &buf[..CANDIDATE_MSG_MAGIC.len()] != CANDIDATE_MSG_MAGIC {
        return None;
    }
    if buf[7..23] != cfg.token {
        return None;
    }
    let sid_len = u16::from_be_bytes([buf[23], buf[24]]) as usize;
    if buf.len() < 25 + sid_len + 1 {
        return None;
    }
    if &buf[25..25 + sid_len] != cfg.session_id.as_bytes() {
        return None;
    }
    let mut off = 25 + sid_len;
    let count = buf[off] as usize;
    off += 1;
    if count > MAX_CANDIDATES || buf.len() < off + count * 7 {
        return None;
    }
    let mut out = Vec::with_capacity(count);
    for _ in 0..count {
        let kind = match buf[off] {
            1 => CandidateKind::Host,
            2 => CandidateKind::ServerReflexive,
            3 => CandidateKind::Relay,
            _ => return None,
        };
        let ip = u32::from_be_bytes([buf[off + 1], buf[off + 2], buf[off + 3], buf[off + 4]]);
        let port = u16::from_be_bytes([buf[off + 5], buf[off + 6]]);
        out.push(Candidate {
            kind,
            addr: SocketAddr::new(IpAddr::V4(Ipv4Addr::from(ip)), port),
        });
        off += 7;
    }
    Some(out)
}

#[cfg(test)]
mod codec_tests {
    use super::*;

    #[test]
    fn candidate_exchange_roundtrip_and_token_check() {
        let cfg = P2PConfig::new("ses-cand", [3u8; 16], Duration::from_secs(1));
        let cands = vec![Candidate {
            kind: CandidateKind::Host,
            addr: "127.0.0.1:43210".parse().unwrap(),
        }];
        let buf = encode_candidates(&cfg, &cands);
        let got = decode_candidates(&buf, &cfg).expect("decode");
        assert_eq!(got, cands);

        let mut wrong = cfg.clone();
        wrong.token = [4u8; 16];
        assert!(decode_candidates(&buf, &wrong).is_none());
        let mut wrong_sid = cfg.clone();
        wrong_sid.session_id = "other".into();
        assert!(decode_candidates(&buf, &wrong_sid).is_none());
    }
}
