//! 真实服务链路端到端：登录 → 绑设备 → 登记身份 → 建会话 → 取票据与公钥
//! → 认证握手 → 加密传输 → 解码。
//!
//! 与之前的例子不同：所有数据都来自真实网关（无 mock），
//! 公钥从 /v1/connect/keys 取，而不是硬编码。

use std::net::SocketAddr;
use std::time::Duration;

use er_capture::{Capture, ScreenCapture};
use er_client::{decode_pubkey, encode_pubkey, Gateway};
use er_codec::{EncodeConfig, Encoder, H264Decoder, OpenH264Codec};
use er_session::crypto::CryptoSession;
use er_session::identity::{sign_handshake, verify_handshake, DeviceIdentity, Handshake};
use er_session::relay_client::RelayClient;

const TAG: &[u8] = b"ER-HS:";

fn main() {
    let base = std::env::args().nth(1).unwrap_or_else(|| "http://127.0.0.1:8080".into());
    let relay_addr: SocketAddr = std::env::args()
        .nth(2)
        .unwrap_or_else(|| "127.0.0.1:19121".into())
        .parse()
        .expect("relay addr");

    // ---------- 1. 两个账号登录并绑设备 ----------
    let target_gw = Gateway::new(&base, "", "");
    target_gw.send_sms("13700003001").expect("sms");
    let target_tok = target_gw.login("13700003001", "123456").expect("target login");
    let target_gw = Gateway::new(&base, &target_tok, "");
    let target_dev = target_gw.bind_device("macos").expect("target bind");

    let ctrl_gw = Gateway::new(&base, "", "");
    ctrl_gw.send_sms("13700003002").expect("sms");
    let ctrl_tok = ctrl_gw.login("13700003002", "123456").expect("ctrl login");
    let ctrl_gw = Gateway::new(&base, &ctrl_tok, "");
    let ctrl_dev = ctrl_gw.bind_device("macos").expect("ctrl bind");
    println!("bound: target={} ctrl={}", target_dev.device_id, ctrl_dev.device_id);

    // ---------- 2. 登记身份公钥 ----------
    let target_id = DeviceIdentity::from_seed(&[31u8; 32]);
    let ctrl_id = DeviceIdentity::from_seed(&[32u8; 32]);

    let target_gw = Gateway::new(&base, &target_tok, &target_dev.device_token);
    target_gw
        .register_identity(&target_dev.device_id, &encode_pubkey(target_id.public().as_bytes()))
        .expect("register target identity");
    let ctrl_gw = Gateway::new(&base, &ctrl_tok, &ctrl_dev.device_token);
    ctrl_gw
        .register_identity(&ctrl_dev.device_id, &encode_pubkey(ctrl_id.public().as_bytes()))
        .expect("register ctrl identity");
    println!("identities registered");

    // ---------- 3. 建会话 ----------
    let target_code = target_dev.raw_code.replace(" ", "");
    let captcha = target_gw.rotate_code(&target_code).expect("rotate");
    let session_id = ctrl_gw.request_connect(&target_code, &captcha).expect("connect request");
    println!("session {} requested", session_id);

    let state = target_gw.consent(&session_id, true).expect("consent");
    println!("consent state {}", state);

    // ---------- 4. 取票据与公钥 ----------
    let target_ticket = target_gw.relay_ticket(&session_id).expect("target ticket");
    let ctrl_ticket = ctrl_gw.relay_ticket(&session_id).expect("ctrl ticket");
    let keys = ctrl_gw.session_keys(&session_id).expect("session keys");

    // 公钥必须与登记值一致——整条信任链的关键断言
    if keys.controller_pubkey != encode_pubkey(ctrl_id.public().as_bytes()) {
        println!("FAIL: controller pubkey from service does not match registered");
        std::process::exit(1);
    }
    if keys.target_pubkey != encode_pubkey(target_id.public().as_bytes()) {
        println!("FAIL: target pubkey mismatch");
        std::process::exit(1);
    }
    println!("public keys from server match registered identities");

    // ---------- 5. 中继注册 ----------
    let mut ctrl_relay =
        RelayClient::new(relay_addr, &session_id, &ctrl_dev.device_id).expect("ctrl relay");
    let mut target_relay =
        RelayClient::new(relay_addr, &session_id, &target_dev.device_id).expect("target relay");
    ctrl_relay.register(&ctrl_ticket).expect("ctrl register");
    target_relay.register(&target_ticket).expect("target register");
    std::thread::sleep(Duration::from_millis(400));

    // ---------- 6. 认证握手 ----------
    let (mut ctrl_crypto, ctrl_eph) = CryptoSession::new(&session_id, true);
    let (mut target_crypto, target_eph) = CryptoSession::new(&session_id, false);

    let ctrl_hs = sign_handshake(&ctrl_id, &session_id, &ctrl_eph, "operator");
    let mut msg = TAG.to_vec();
    msg.extend_from_slice(&ctrl_hs.encode());
    ctrl_relay.send(&msg).expect("send ctrl hs");

    let target_hs = sign_handshake(&target_id, &session_id, &target_eph, "controlled");
    let mut msg2 = TAG.to_vec();
    msg2.extend_from_slice(&target_hs.encode());
    target_relay.send(&msg2).expect("send target hs");

    let saw_target =
        ctrl_relay.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no target hs");
    let saw_ctrl =
        target_relay.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no ctrl hs");

    let peer_target = Handshake::decode(&saw_target[TAG.len()..]).expect("decode target hs");
    let peer_ctrl = Handshake::decode(&saw_ctrl[TAG.len()..]).expect("decode ctrl hs");

    if verify_handshake(&target_id.public(), &session_id, &peer_target, "controlled").is_err() {
        println!("FAIL: target signature invalid");
        std::process::exit(1);
    }
    if verify_handshake(&ctrl_id.public(), &session_id, &peer_ctrl, "operator").is_err() {
        println!("FAIL: ctrl signature invalid");
        std::process::exit(1);
    }
    println!("handshake verified against server-provided keys");

    ctrl_crypto.derive_keys(&peer_target.ephemeral_key(), true);
    target_crypto.derive_keys(&peer_ctrl.ephemeral_key(), false);

    // ---------- 7. 采集 → 编码 → 加密 → 中继 → 解密 → 解码 ----------
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

    let sealed = ctrl_crypto.seal(&encoded);
    ctrl_relay.send(&sealed).expect("send sealed");

    let got =
        target_relay.recv_timeout(Duration::from_secs(3)).expect("recv").expect("no payload");
    let plain = target_crypto.open(&got).expect("decrypt");
    if plain != encoded {
        println!("FAIL: payload mismatch");
        std::process::exit(1);
    }

    let mut dec = H264Decoder::new().expect("decoder");
    match dec.decode(&plain) {
        Ok(Some((w, h, _rgb))) => {
            println!("decoded {}x{}", w, h);
            if w != frame.width as usize || h != frame.height as usize {
                println!("FAIL: dimension mismatch");
                std::process::exit(1);
            }
            println!("FULL SESSION E2E PASSED (real gateway, real relay, authenticated E2E encryption)");
        }
        _ => {
            println!("FAIL: decode failed");
            std::process::exit(1);
        }
    }
}
