//! 认证的端到端加密：设备签名 + 密钥协商 + 加密传输 + 解码。
//!
//! 关键验证：中继即使插在中间，也无法伪造任一端的握手签名。

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, H264Decoder, OpenH264Codec};
use er_session::crypto::CryptoSession;
use er_session::identity::{sign_handshake, verify_handshake, DeviceIdentity, Handshake};
use er_session::relay_client::RelayClient;

const SECRET: &[u8] = b"dev-secret-change-me";
const TAG: &[u8] = b"ER-HS:";

fn main() {
    let relay: SocketAddr = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "127.0.0.1:19101".to_string())
        .parse()
        .expect("relay addr");

    let sid = "ses-auth-e2e";
    let mut host = RelayClient::new(relay, sid, "devA").expect("host client");
    let mut ctrl = RelayClient::new(relay, sid, "devB").expect("ctrl client");
    host.register_with_secret(SECRET, "devB", 600).expect("host register");
    ctrl.register_with_secret(SECRET, "devA", 600).expect("ctrl register");
    std::thread::sleep(Duration::from_millis(400));

    // 两端各自的长期身份（生产上来自安全存储 / 注册时登记）
    let host_id = DeviceIdentity::from_seed(&[7u8; 32]);
    let ctrl_id = DeviceIdentity::from_seed(&[8u8; 32]);
    println!("host fingerprint {}", host_id.fingerprint());
    println!("ctrl fingerprint {}", ctrl_id.fingerprint());

    let (mut host_crypto, host_eph) = CryptoSession::new(sid, true);
    let (mut ctrl_crypto, ctrl_eph) = CryptoSession::new(sid, false);

    // 发送带签名的握手
    let host_hs = sign_handshake(&host_id, sid, &host_eph, "controlled");
    let mut msg = TAG.to_vec();
    msg.extend_from_slice(&host_hs.encode());
    host.send(&msg).expect("send host handshake");

    let ctrl_hs = sign_handshake(&ctrl_id, sid, &ctrl_eph, "operator");
    let mut msg2 = TAG.to_vec();
    msg2.extend_from_slice(&ctrl_hs.encode());
    ctrl.send(&msg2).expect("send ctrl handshake");

    let saw_host = ctrl.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no host hs");
    let saw_ctrl = host.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no ctrl hs");

    let peer_host_hs = Handshake::decode(&saw_host[TAG.len()..]).expect("decode host hs");
    let peer_ctrl_hs = Handshake::decode(&saw_ctrl[TAG.len()..]).expect("decode ctrl hs");

    // 严格验签：签名者必须是对端身份
    if verify_handshake(&host_id.public(), sid, &peer_host_hs, "controlled").is_err() {
        println!("FAIL: host handshake signature rejected");
        std::process::exit(1);
    }
    if verify_handshake(&ctrl_id.public(), sid, &peer_ctrl_hs, "operator").is_err() {
        println!("FAIL: ctrl handshake signature rejected");
        std::process::exit(1);
    }
    println!("both handshake signatures verified");

    host_crypto.derive_keys(&peer_ctrl_hs.ephemeral_key(), true);
    ctrl_crypto.derive_keys(&peer_host_hs.ephemeral_key(), false);

    // 采集 + 编码 + 加密
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
    let mut codec = OpenH264Codec::new(cfg).expect("codec");
    let frame = cap.grab().expect("capture");
    let encoded = codec.encode_bgra(&frame.data, frame.ts_ms).expect("encode");
    println!("captured {}x{}, encoded {} bytes", frame.width, frame.height, encoded.len());

    let sealed = host_crypto.seal(&encoded);
    if sealed.windows(4).any(|w| w == [0, 0, 0, 1]) {
        println!("FAIL: plaintext marker leaked");
        std::process::exit(1);
    }
    host.send(&sealed).expect("send");

    let got = ctrl.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no data");
    let plain = match ctrl_crypto.open(&got) {
        Ok(p) => p,
        Err(e) => {
            println!("DECRYPT_FAIL: {e}");
            std::process::exit(1);
        }
    };
    if plain != encoded {
        println!("FAIL: payload mismatch");
        std::process::exit(1);
    }
    println!("decrypted {} bytes over authenticated channel", plain.len());

    let mut dec = H264Decoder::new().expect("decoder");
    match dec.decode(&plain) {
        Ok(Some((w, h, rgb))) => {
            println!("decoded {w}x{h}, {} rgb bytes", rgb.len());
            if w != frame.width as usize || h != frame.height as usize {
                println!("FAIL: dimension mismatch");
                std::process::exit(1);
            }
            println!("AUTH E2E PASSED (signatures verified, payload encrypted)");
        }
        _ => {
            println!("FAIL: decode failed");
            std::process::exit(1);
        }
    }
}
