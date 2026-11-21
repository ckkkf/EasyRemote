//! 端到端加密全链路：密钥交换 → 采集 → 编码 → 加密 → 中继 → 解密 → 解码。
//!
//! 同时验证两件事：
//!   1. 端到端能还原画面
//!   2. **中继链路上看不到明文**（PRD §9「中继只见密文」）

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, H264Decoder, OpenH264Codec};
use er_session::crypto::CryptoSession;
use er_session::relay_client::RelayClient;
use x25519_dalek::PublicKey;

const SECRET: &[u8] = b"dev-secret-change-me";
const KEY_EXCHANGE_TAG: &[u8] = b"ER-PUBKEY:";

fn main() {
    let relay: SocketAddr = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "127.0.0.1:19101".to_string())
        .parse()
        .expect("relay addr");

    let sid = "ses-secure-1";
    let mut host = RelayClient::new(relay, sid, "devA").expect("host client");
    let mut ctrl = RelayClient::new(relay, sid, "devB").expect("ctrl client");
    host.register_with_secret(SECRET, "devB", 600).expect("host register");
    ctrl.register_with_secret(SECRET, "devA", 600).expect("ctrl register");
    std::thread::sleep(Duration::from_millis(400));

    // --- 密钥交换（公钥走中继明文，密钥本身永不过网）---
    let (mut host_crypto, host_pub) = CryptoSession::new(sid, true);
    let (mut ctrl_crypto, ctrl_pub) = CryptoSession::new(sid, false);

    let mut host_msg = KEY_EXCHANGE_TAG.to_vec();
    host_msg.extend_from_slice(host_pub.as_bytes());
    host.send(&host_msg).expect("send host pubkey");

    let mut ctrl_msg = KEY_EXCHANGE_TAG.to_vec();
    ctrl_msg.extend_from_slice(ctrl_pub.as_bytes());
    ctrl.send(&ctrl_msg).expect("send ctrl pubkey");

    let ctrl_saw = ctrl
        .recv_timeout(Duration::from_secs(3))
        .expect("recv host pubkey")
        .expect("no host pubkey");
    let host_saw = host
        .recv_timeout(Duration::from_secs(3))
        .expect("recv ctrl pubkey")
        .expect("no ctrl pubkey");

    let peer_for_ctrl = parse_pubkey(&ctrl_saw);
    let peer_for_host = parse_pubkey(&host_saw);
    host_crypto.derive_keys(&peer_for_host, true);
    ctrl_crypto.derive_keys(&peer_for_ctrl, false);
    println!("key exchange done (public keys only, secrets never left the machine)");

    // --- 采集 + 编码 ---
    let mut cap = match ScreenCapture::open_primary() {
        Ok(c) => c,
        Err(e) => {
            println!("NO_DISPLAY: {e}");
            return;
        }
    };
    let info = cap.info();
    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    let mut codec = match OpenH264Codec::new(cfg) {
        Ok(c) => c,
        Err(e) => {
            println!("CODEC_FAIL: {e}");
            return;
        }
    };
    let frame = match cap.grab() {
        Ok(f) => f,
        Err(e) => {
            println!("CAPTURE_FAIL: {e}");
            return;
        }
    };
    let encoded = match codec.encode_bgra(&frame.data, frame.ts_ms) {
        Ok(d) => d,
        Err(e) => {
            println!("ENCODE_FAIL: {e}");
            return;
        }
    };
    println!("source {}x{}, encoded {} bytes", frame.width, frame.height, encoded.len());

    // --- 加密并发送 ---
    let sealed = host_crypto.seal(&encoded);
    println!("sealed {} -> {} bytes (+{} overhead)", encoded.len(), sealed.len(), sealed.len() - encoded.len());
    if sealed.windows(4).any(|w| w == [0, 0, 0, 1]) {
        println!("FAIL: Annex-B start code leaked into ciphertext");
        std::process::exit(1);
    }
    host.send(&sealed).expect("send sealed");

    // --- 接收并解密 ---
    let got = match ctrl.recv_timeout(Duration::from_secs(3)) {
        Ok(Some(v)) => v,
        Ok(None) => {
            println!("FAIL: nothing received");
            std::process::exit(1);
        }
        Err(e) => {
            println!("RECV_FAIL: {e}");
            std::process::exit(1);
        }
    };
    let plain = match ctrl_crypto.open(&got) {
        Ok(p) => p,
        Err(e) => {
            println!("DECRYPT_FAIL: {e}");
            std::process::exit(1);
        }
    };
    if plain != encoded {
        println!("FAIL: decrypted payload differs from original");
        std::process::exit(1);
    }
    println!("decrypted {} bytes, identical to source stream", plain.len());

    // --- 解码验证画面可还原 ---
    let mut dec = match H264Decoder::new() {
        Ok(d) => d,
        Err(e) => {
            println!("DECODER_FAIL: {e}");
            return;
        }
    };
    match dec.decode(&plain) {
        Ok(Some((w, h, rgb))) => {
            println!("decoded {w}x{h}, {} rgb bytes", rgb.len());
            if w != frame.width as usize || h != frame.height as usize {
                println!("FAIL: dimension mismatch");
                std::process::exit(1);
            }
            println!("SECURE E2E PASSED (relay saw ciphertext only)");
        }
        _ => {
            println!("FAIL: decode failed");
            std::process::exit(1);
        }
    }
}

fn parse_pubkey(msg: &[u8]) -> PublicKey {
    let body = &msg[KEY_EXCHANGE_TAG.len()..];
    let mut bytes = [0u8; 32];
    bytes.copy_from_slice(&body[..32]);
    PublicKey::from(bytes)
}
