//! 端到端加密（PRD §9）：中继只见密文。
//!
//! 方案：X25519 临时密钥协商 → HKDF-SHA256 派生双向密钥 →
//! ChaCha20-Poly1305 AEAD 逐包加密。
//!
//! 关键设计：
//!   - **每个方向独立密钥**，避免两个方向共用一个 nonce 空间
//!   - nonce 是 96 位单调计数器，接收侧做重放检查（必须严格递增）
//!   - 关联数据绑定 session_id，防止密文被搬到别的会话复用

use chacha20poly1305::aead::{Aead, KeyInit, Payload};
use chacha20poly1305::{ChaCha20Poly1305, Key, Nonce};
use hkdf::Hkdf;
use sha2::Sha256;
use x25519_dalek::{PublicKey, StaticSecret};

/// 密钥长度。
pub const KEY_LEN: usize = 32;
/// nonce 长度。
pub const NONCE_LEN: usize = 12;

/// 加密错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CryptoError {
    /// AEAD 校验失败（被篡改或密钥不对）
    DecryptFailed,
    /// 重放或乱序（nonce 不大于已见最大值）
    Replayed,
    /// 密文太短
    Truncated,
}

impl std::fmt::Display for CryptoError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CryptoError::DecryptFailed => write!(f, "decrypt failed"),
            CryptoError::Replayed => write!(f, "replayed packet"),
            CryptoError::Truncated => write!(f, "truncated ciphertext"),
        }
    }
}

impl std::error::Error for CryptoError {}

/// 一端持有的会话密钥材料。
pub struct CryptoSession {
    secret: StaticSecret,
    /// 发送用密钥
    tx_key: [u8; KEY_LEN],
    /// 接收用密钥
    rx_key: [u8; KEY_LEN],
    tx_nonce: u64,
    rx_last: Option<u64>,
    /// 关联数据（session id）
    aad: Vec<u8>,
}

impl CryptoSession {
    /// 生成临时密钥对，返回本端公钥。
    pub fn new(session_id: &str, initiator: bool) -> (Self, PublicKey) {
        let secret = StaticSecret::random_from_rng(rand_core::OsRng);
        let public = PublicKey::from(&secret);
        let sess = Self {
            secret,
            tx_key: [0u8; KEY_LEN],
            rx_key: [0u8; KEY_LEN],
            tx_nonce: 0,
            rx_last: None,
            aad: session_id.as_bytes().to_vec(),
        };
        // 调用方随后必须调用 derive_keys 完成派生
        let _ = initiator;
        (sess, public)
    }

    /// 用对端公钥派生双向密钥。
    ///
    /// `initiator` 决定 tx/rx 的分配：两端必须传相反的值，
    /// 否则会出现「两边都用同一把密钥发」的隐蔽错误。
    pub fn derive_keys(&mut self, peer: &PublicKey, initiator: bool) {
        let shared = self.secret.diffie_hellman(peer);
        let hk = Hkdf::<Sha256>::new(Some(&self.aad), shared.as_bytes());
        let mut okm = [0u8; KEY_LEN * 2];
        hk.expand(b"easyremote-session-v1", &mut okm)
            .expect("hkdf output length is valid");

        let (a, b) = okm.split_at(KEY_LEN);
        if initiator {
            self.tx_key.copy_from_slice(a);
            self.rx_key.copy_from_slice(b);
        } else {
            self.tx_key.copy_from_slice(b);
            self.rx_key.copy_from_slice(a);
        }
    }

    /// 加密一个包。返回 nonce(12) + 密文。
    pub fn seal(&mut self, plaintext: &[u8]) -> Vec<u8> {
        let cipher = ChaCha20Poly1305::new(Key::from_slice(&self.tx_key));
        let nonce_bytes = nonce_for(self.tx_nonce);
        let out = cipher
            .encrypt(
                Nonce::from_slice(&nonce_bytes),
                Payload { msg: plaintext, aad: &self.aad },
            )
            .expect("chacha20poly1305 encrypt never fails for valid inputs");
        self.tx_nonce += 1;

        let mut framed = Vec::with_capacity(NONCE_LEN + out.len());
        framed.extend_from_slice(&nonce_bytes);
        framed.extend_from_slice(&out);
        framed
    }

    /// 解密一个包（nonce 前缀格式）。重放会被拒绝。
    pub fn open(&mut self, sealed: &[u8]) -> Result<Vec<u8>, CryptoError> {
        if sealed.len() < NONCE_LEN + 16 {
            return Err(CryptoError::Truncated);
        }
        let (nonce_bytes, ct) = sealed.split_at(NONCE_LEN);
        let counter = counter_of(nonce_bytes);
        // 首包计数器为 0，用 Option 表示「还没收过」，否则第一包会被误判重放
        if let Some(last) = self.rx_last {
            if counter <= last {
                return Err(CryptoError::Replayed);
            }
        }
        let cipher = ChaCha20Poly1305::new(Key::from_slice(&self.rx_key));
        let pt = cipher
            .decrypt(
                Nonce::from_slice(nonce_bytes),
                Payload { msg: ct, aad: &self.aad },
            )
            .map_err(|_| CryptoError::DecryptFailed)?;
        self.rx_last = Some(counter);
        Ok(pt)
    }

    /// 已发送包数。
    pub fn sent(&self) -> u64 {
        self.tx_nonce
    }
}

fn nonce_for(counter: u64) -> [u8; NONCE_LEN] {
    let mut n = [0u8; NONCE_LEN];
    n[4..].copy_from_slice(&counter.to_be_bytes());
    n
}

fn counter_of(nonce: &[u8]) -> u64 {
    let mut buf = [0u8; 8];
    buf.copy_from_slice(&nonce[4..12]);
    u64::from_be_bytes(buf)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pair(session: &str) -> (CryptoSession, CryptoSession) {
        let (mut a, a_pub) = CryptoSession::new(session, true);
        let (mut b, b_pub) = CryptoSession::new(session, false);
        a.derive_keys(&b_pub, true);
        b.derive_keys(&a_pub, false);
        (a, b)
    }

    #[test]
    fn roundtrip_both_directions() {
        let (mut a, mut b) = pair("ses-1");
        let ct = a.seal(b"hello");
        assert_eq!(b.open(&ct).unwrap(), b"hello");

        let ct2 = b.seal(b"world");
        assert_eq!(a.open(&ct2).unwrap(), b"world");
    }

    #[test]
    fn ciphertext_differs_from_plaintext() {
        let (mut a, _b) = pair("ses-1");
        let plain = b"H264-PLAINTEXT-MARKER";
        let ct = a.seal(plain);
        assert!(
            !ct.windows(plain.len()).any(|w| w == plain),
            "plaintext must not appear in ciphertext"
        );
    }

    #[test]
    fn tampering_detected() {
        let (mut a, mut b) = pair("ses-1");
        let mut ct = a.seal(b"important");
        let last = ct.len() - 1;
        ct[last] ^= 0x01;
        assert_eq!(b.open(&ct), Err(CryptoError::DecryptFailed));
    }

    #[test]
    fn replay_rejected() {
        let (mut a, mut b) = pair("ses-1");
        let ct = a.seal(b"once");
        assert!(b.open(&ct).is_ok());
        assert_eq!(b.open(&ct), Err(CryptoError::Replayed), "replay must be rejected");
    }

    #[test]
    fn different_session_aad_breaks_decryption() {
        // A 属于 ses-1，B 属于 ses-2：即使密钥协商成功，AAD 不同也必须失败
        let (mut a, a_pub) = CryptoSession::new("ses-1", true);
        let (mut b, b_pub) = CryptoSession::new("ses-2", false);
        a.derive_keys(&b_pub, true);
        b.derive_keys(&a_pub, false);
        let ct = a.seal(b"data");
        assert_eq!(b.open(&ct), Err(CryptoError::DecryptFailed));
    }

    #[test]
    fn truncated_rejected() {
        let (_a, mut b) = pair("ses-1");
        assert_eq!(b.open(&[0u8; 4]), Err(CryptoError::Truncated));
    }

    #[test]
    fn keys_are_directional() {
        // 用同一把密钥发两次，两端计数独立递增，互不干扰
        let (mut a, mut b) = pair("ses-1");
        for i in 0..5u8 {
            let ct = a.seal(&[i]);
            assert_eq!(b.open(&ct).unwrap(), vec![i]);
        }
        assert_eq!(a.sent(), 5);
    }

    #[test]
    fn wrong_direction_key_fails() {
        let (mut a, mut b) = pair("ses-1");
        let ct = a.seal(b"x");
        // 让 a 尝试解自己的包：tx/rx 不同，必须失败
        assert!(a.open(&ct).is_err(), "sender must not decrypt with its own tx key");
        assert!(b.open(&ct).is_ok());
    }
}
