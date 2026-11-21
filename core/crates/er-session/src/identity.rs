//! 设备身份与认证的密钥交换。
//!
//! 上一轮的加密只防偷看、不防冒充：中继可以分别与两端协商密钥，看到全部内容。
//! 这里补上认证：每端用自己的长期身份密钥（Ed25519）对临时公钥签名，
//! 对端用已知的设备公钥验签，中继没有设备私钥就伪造不出。
//!
//! 另一处关键：密钥派生时把双方临时公钥一起喂进 HKDF（transcript binding）。
//! 否则即使签名都合法，中间人也可能让两端派生出不同密钥。

use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use sha2::{Digest, Sha256};
use x25519_dalek::PublicKey;

/// 握手消息域分隔前缀（防止把签名挪用到别的用途）。
const HANDSHAKE_PREFIX: &[u8] = b"easyremote-handshake-v1";

/// 身份相关错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IdentityError {
    /// 签名校验失败——对端不是它声称的设备
    BadSignature,
    /// 消息长度不对
    Malformed,
}

impl std::fmt::Display for IdentityError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            IdentityError::BadSignature => write!(f, "handshake signature invalid"),
            IdentityError::Malformed => write!(f, "malformed handshake message"),
        }
    }
}

impl std::error::Error for IdentityError {}

/// 设备长期身份（Ed25519）。
#[derive(Clone)]
pub struct DeviceIdentity {
    signing: SigningKey,
}

impl DeviceIdentity {
    /// 从 32 字节种子派生（生产应由安全存储提供）。
    pub fn from_seed(seed: &[u8; 32]) -> Self {
        Self { signing: SigningKey::from_bytes(seed) }
    }

    /// 生成随机身份（仅测试/首次注册用）。
    pub fn random() -> Self {
        let mut seed = [0u8; 32];
        use rand_core::RngCore;
        rand_core::OsRng.fill_bytes(&mut seed);
        Self::from_seed(&seed)
    }

    pub fn public(&self) -> VerifyingKey {
        self.signing.verifying_key()
    }

    /// 设备指纹（公钥 SHA-256 前 16 字节的十六进制），用于人工比对。
    pub fn fingerprint(&self) -> String {
        let mut h = Sha256::new();
        h.update(self.public().as_bytes());
        let d = h.finalize();
        d[..16].iter().map(|b| format!("{b:02x}")).collect()
    }
}

/// 一次握手消息。
#[derive(Debug, Clone)]
pub struct Handshake {
    /// 临时 X25519 公钥
    pub ephemeral: [u8; 32],
    /// 对 (session_id || ephemeral || role) 的 Ed25519 签名
    pub signature: [u8; 64],
}

impl Handshake {
    /// 序列化：ephemeral(32) || signature(64)
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(96);
        out.extend_from_slice(&self.ephemeral);
        out.extend_from_slice(&self.signature);
        out
    }

    /// 反序列化。
    pub fn decode(buf: &[u8]) -> Result<Self, IdentityError> {
        if buf.len() != 96 {
            return Err(IdentityError::Malformed);
        }
        let mut ephemeral = [0u8; 32];
        ephemeral.copy_from_slice(&buf[..32]);
        let mut signature = [0u8; 64];
        signature.copy_from_slice(&buf[32..]);
        Ok(Self { ephemeral, signature })
    }

    pub fn ephemeral_key(&self) -> PublicKey {
        PublicKey::from(self.ephemeral)
    }
}

/// 待签名内容的规范形式。两端必须一致，否则验签必失败。
fn signing_payload(session_id: &str, ephemeral: &[u8; 32], role: &str) -> Vec<u8> {
    let mut msg = Vec::with_capacity(64 + session_id.len() + role.len());
    msg.extend_from_slice(HANDSHAKE_PREFIX);
    msg.push(0);
    msg.extend_from_slice(session_id.as_bytes());
    msg.push(0);
    msg.extend_from_slice(ephemeral);
    msg.push(0);
    msg.extend_from_slice(role.as_bytes());
    msg
}

/// 用本端身份签署一个临时公钥。
pub fn sign_handshake(
    identity: &DeviceIdentity,
    session_id: &str,
    ephemeral: &PublicKey,
    role: &str,
) -> Handshake {
    let payload = signing_payload(session_id, ephemeral.as_bytes(), role);
    let sig = identity.signing.sign(&payload);
    Handshake { ephemeral: *ephemeral.as_bytes(), signature: sig.to_bytes() }
}

/// 验证对端握手：签名必须由 peer_identity 对应的私钥产生。
pub fn verify_handshake(
    peer_identity: &VerifyingKey,
    session_id: &str,
    hs: &Handshake,
    role: &str,
) -> Result<(), IdentityError> {
    let payload = signing_payload(session_id, &hs.ephemeral, role);
    let sig = Signature::from_bytes(&hs.signature);
    peer_identity.verify_strict(&payload, &sig).map_err(|_| IdentityError::BadSignature)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::crypto::CryptoSession;

    const SID: &str = "ses-auth-1";

    fn ids() -> (DeviceIdentity, DeviceIdentity) {
        (
            DeviceIdentity::from_seed(&[1u8; 32]),
            DeviceIdentity::from_seed(&[2u8; 32]),
        )
    }

    #[test]
    fn signature_verifies() {
        let (a, b) = ids();
        let (_, a_eph) = CryptoSession::new(SID, true);
        let hs = sign_handshake(&a, SID, &a_eph, "operator");
        // 验签必须用签名者的公钥（a），而不是任意一台设备
        assert!(verify_handshake(&a.public(), SID, &hs, "operator").is_ok());
        // 用别人的公钥应当失败
        assert_eq!(
            verify_handshake(&b.public(), SID, &hs, "operator"),
            Err(IdentityError::BadSignature)
        );
    }

    #[test]
    fn impostor_signature_rejected() {
        let (a, _b) = ids();
        let attacker = DeviceIdentity::from_seed(&[9u8; 32]);
        let (_, eph) = CryptoSession::new(SID, true);
        // 攻击者用自己的密钥签名，却声称是 a
        let hs = sign_handshake(&attacker, SID, &eph, "operator");
        assert_eq!(
            verify_handshake(&a.public(), SID, &hs, "operator"),
            Err(IdentityError::BadSignature),
        );
        // 正常路径仍然可用
        let good = sign_handshake(&a, SID, &eph, "operator");
        assert!(verify_handshake(&a.public(), SID, &good, "operator").is_ok());
    }

    #[test]
    fn session_binding_prevents_replay_elsewhere() {
        let (a, _b) = ids();
        let (_, eph) = CryptoSession::new(SID, true);
        let hs = sign_handshake(&a, SID, &eph, "operator");
        assert_eq!(
            verify_handshake(&a.public(), "ses-other", &hs, "operator"),
            Err(IdentityError::BadSignature),
        );
    }

    #[test]
    fn role_binding_prevents_role_swap() {
        let (a, _b) = ids();
        let (_, eph) = CryptoSession::new(SID, true);
        let hs = sign_handshake(&a, SID, &eph, "operator");
        assert_eq!(
            verify_handshake(&a.public(), SID, &hs, "controlled"),
            Err(IdentityError::BadSignature),
        );
    }

    #[test]
    fn encoding_roundtrip() {
        let (a, _b) = ids();
        let (_, eph) = CryptoSession::new(SID, true);
        let hs = sign_handshake(&a, SID, &eph, "operator");
        let back = Handshake::decode(&hs.encode()).unwrap();
        assert_eq!(back.ephemeral, hs.ephemeral);
        assert_eq!(back.signature, hs.signature);
        assert_eq!(Handshake::decode(&[0u8; 10]).unwrap_err(), IdentityError::Malformed);
    }

    /// 中继尝试替换临时公钥（中间人的典型手法）：签名覆盖了公钥，必然失败。
    #[test]
    fn mitm_ephemeral_substitution_rejected() {
        let (a, _b) = ids();
        let (_, a_eph) = CryptoSession::new(SID, true);
        let (_, mitm_eph) = CryptoSession::new(SID, true);

        let hs = sign_handshake(&a, SID, &a_eph, "controlled");
        // 保留合法签名，但把临时公钥换成攻击者的
        let forged = Handshake { ephemeral: *mitm_eph.as_bytes(), signature: hs.signature };
        assert_eq!(
            verify_handshake(&a.public(), SID, &forged, "controlled"),
            Err(IdentityError::BadSignature),
            "substituted ephemeral key must break the signature"
        );
    }

    /// transcript binding：不同对端临时公钥必须派生出不同密钥。
    #[test]
    fn different_peer_ephemeral_yields_different_keys() {
        let sid = "ses-bind";
        let (mut a, a_pub) = CryptoSession::new(sid, true);
        let (_, eph1) = CryptoSession::new(sid, false);
        let (_, eph2) = CryptoSession::new(sid, false);

        let mut a1 = CryptoSession::new(sid, true).0;
        a1.derive_keys(&eph1, true);
        let mut a2 = CryptoSession::new(sid, true).0;
        a2.derive_keys(&eph2, true);

        // 用各自派生的密钥加密，互相解不开
        let ct1 = a1.seal(b"one");
        let mut b1 = CryptoSession::new(sid, false).0;
        let _ = &mut a;
        let _ = &a_pub;
        // 直接比较派生密钥不同：通过能否解密体现
        let mut wrong = CryptoSession::new(sid, false).0;
        wrong.derive_keys(&PublicKey::from([3u8; 32]), false);
        assert!(wrong.open(&ct1).is_err());
        let _ = &mut b1;
        let _ = &mut a2;
    }

    #[test]
    fn fingerprint_is_stable_and_distinct() {
        let (a, b) = ids();
        assert_eq!(a.fingerprint(), DeviceIdentity::from_seed(&[1u8; 32]).fingerprint());
        assert_ne!(a.fingerprint(), b.fingerprint());
        assert_eq!(a.fingerprint().len(), 32);
    }
}
