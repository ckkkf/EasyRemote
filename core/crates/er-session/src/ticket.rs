//! 中继票据：与 Go `relaymgr` **逐字节兼容**。
//!
//! 格式：`base64url_nopad(sessionID|self|peer|exp) . base64url_nopad(HMAC-SHA256(payload))`
//! 两边不一致会导致客户端能连上 UDP 端口却永远注册失败，所以这里单测严格对齐字段顺序。

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use hmac::{Hmac, Mac};
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

/// 票据。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Ticket {
    pub session_id: String,
    pub this_device: String,
    pub peer_device: String,
    pub expires_at_unix: i64,
}

/// 票据错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TicketError {
    Malformed,
    BadSignature,
    Expired,
}

impl std::fmt::Display for TicketError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TicketError::Malformed => write!(f, "malformed ticket"),
            TicketError::BadSignature => write!(f, "bad signature"),
            TicketError::Expired => write!(f, "ticket expired"),
        }
    }
}

impl std::error::Error for TicketError {}

fn mac(secret: &[u8], payload: &str) -> String {
    let mut m = HmacSha256::new_from_slice(secret).expect("hmac accepts any key length");
    m.update(payload.as_bytes());
    URL_SAFE_NO_PAD.encode(m.finalize().into_bytes())
}

/// 签发票据（测试与本地自托管用；生产由网关签发）。
pub fn issue(
    secret: &[u8],
    session_id: &str,
    this_device: &str,
    peer_device: &str,
    exp: i64,
) -> String {
    let payload = format!("{session_id}|{this_device}|{peer_device}|{exp}");
    let token = format!(
        "{}.{}",
        URL_SAFE_NO_PAD.encode(payload.as_bytes()),
        mac(secret, &payload)
    );
    token
}

/// 校验票据。
pub fn verify(secret: &[u8], token: &str, now_unix: i64) -> Result<Ticket, TicketError> {
    let (body, sig) = token.split_once('.').ok_or(TicketError::Malformed)?;
    let raw = URL_SAFE_NO_PAD
        .decode(body.as_bytes())
        .map_err(|_| TicketError::Malformed)?;
    let payload = String::from_utf8(raw).map_err(|_| TicketError::Malformed)?;
    if mac(secret, &payload) != sig {
        return Err(TicketError::BadSignature);
    }
    let f: Vec<&str> = payload.split('|').collect();
    if f.len() != 4 {
        return Err(TicketError::Malformed);
    }
    let exp: i64 = f[3].parse().map_err(|_| TicketError::Malformed)?;
    if now_unix > exp {
        return Err(TicketError::Expired);
    }
    Ok(Ticket {
        session_id: f[0].to_string(),
        this_device: f[1].to_string(),
        peer_device: f[2].to_string(),
        expires_at_unix: exp,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    const SECRET: &[u8] = b"dev-secret-change-me";

    #[test]
    fn issue_and_verify_roundtrip() {
        let tok = issue(SECRET, "ses-1", "devA", "devB", 2_000_000_000);
        let t = verify(SECRET, &tok, 1_900_000_000).unwrap();
        assert_eq!(t.session_id, "ses-1");
        assert_eq!(t.this_device, "devA");
        assert_eq!(t.peer_device, "devB");
    }

    /// 与 Go 实现对齐：payload 字段顺序必须是 sid|self|peer|exp
    #[test]
    fn payload_layout_matches_go() {
        let tok = issue(SECRET, "s", "a", "b", 123);
        let body = tok.split('.').next().unwrap();
        let raw = URL_SAFE_NO_PAD.decode(body).unwrap();
        assert_eq!(String::from_utf8(raw).unwrap(), "s|a|b|123");
    }

    #[test]
    fn tampered_signature_rejected() {
        let tok = issue(SECRET, "s", "a", "b", 123);
        let bad = format!("{}x", tok);
        assert_eq!(verify(SECRET, &bad, 0), Err(TicketError::BadSignature));
    }

    #[test]
    fn wrong_secret_rejected() {
        let tok = issue(SECRET, "s", "a", "b", 123);
        assert_eq!(
            verify(b"another-secret-1234", &tok, 0),
            Err(TicketError::BadSignature)
        );
    }

    #[test]
    fn expired_rejected() {
        let tok = issue(SECRET, "s", "a", "b", 100);
        assert_eq!(verify(SECRET, &tok, 200), Err(TicketError::Expired));
    }

    #[test]
    fn malformed_rejected() {
        assert_eq!(
            verify(SECRET, "not-a-ticket", 0),
            Err(TicketError::Malformed)
        );
        assert_eq!(verify(SECRET, "a.b.c", 0), Err(TicketError::Malformed));
    }
}
