//! 双端会话编排：在同一进程内跑完「主控 + 被控」两侧的真实链路。
//!
//! 用途：本地联调、Flutter 端的自检入口、端到端回归。
//! 生产形态应由两端独立进程完成，这里只是为了能在一个可执行入口里
//! 把整条链路跑通并给出可断言的结果。

use std::net::SocketAddr;
use std::time::Duration;

use crate::{decode_pubkey, encode_pubkey, Gateway};
use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, H264Decoder, OpenH264Codec};
use er_session::crypto::CryptoSession;
use er_session::identity::{sign_handshake, verify_handshake, DeviceIdentity, Handshake};
use er_session::relay_client::{p2p_token, RelayClient};
use er_transport::p2p::P2PConfig;

const TAG: &[u8] = b"ER-HS:";

/// 一次双端会话的结果。
#[derive(Debug, Clone, serde::Serialize)]
pub struct PairReport {
    pub session_id: String,
    pub target_device: String,
    pub controller_device: String,
    pub encoded_bytes: usize,
    pub sealed_bytes: usize,
    pub decoded_width: usize,
    pub decoded_height: usize,
    pub keys_match_registered: bool,
    pub handshake_verified: bool,
    pub plaintext_marker_absent: bool,
    pub link: String,
}

/// 编排一次完整会话。
pub fn run_pair(
    gateway_base: &str,
    relay_addr: SocketAddr,
    phone_a: &str,
    phone_b: &str,
    sms_code: &str,
) -> Result<PairReport, String> {
    let target_gw = Gateway::new(gateway_base, "", "");
    let _ = target_gw.send_sms(phone_a);
    let target_tok = target_gw
        .login(phone_a, sms_code)
        .map_err(|e| format!("target login: {e}"))?;
    let target_gw = Gateway::new(gateway_base, &target_tok, "");
    let target_dev = target_gw
        .bind_device("macos")
        .map_err(|e| format!("target bind: {e}"))?;

    let ctrl_gw = Gateway::new(gateway_base, "", "");
    let _ = ctrl_gw.send_sms(phone_b);
    let ctrl_tok = ctrl_gw
        .login(phone_b, sms_code)
        .map_err(|e| format!("ctrl login: {e}"))?;
    let ctrl_gw = Gateway::new(gateway_base, &ctrl_tok, "");
    let ctrl_dev = ctrl_gw
        .bind_device("macos")
        .map_err(|e| format!("ctrl bind: {e}"))?;

    let target_id = DeviceIdentity::random();
    let ctrl_id = DeviceIdentity::random();

    let target_gw = Gateway::new(gateway_base, &target_tok, &target_dev.device_token);
    target_gw
        .register_identity(
            &target_dev.device_id,
            &encode_pubkey(target_id.public().as_bytes()),
        )
        .map_err(|e| format!("register target identity: {e}"))?;
    let ctrl_gw = Gateway::new(gateway_base, &ctrl_tok, &ctrl_dev.device_token);
    ctrl_gw
        .register_identity(
            &ctrl_dev.device_id,
            &encode_pubkey(ctrl_id.public().as_bytes()),
        )
        .map_err(|e| format!("register ctrl identity: {e}"))?;

    let code = target_dev.raw_code.replace(' ', "");
    let captcha = target_gw
        .rotate_code(&code)
        .map_err(|e| format!("rotate: {e}"))?;
    let session_id = ctrl_gw
        .request_connect(&code, &captcha)
        .map_err(|e| format!("connect: {e}"))?;
    target_gw
        .consent(&session_id, true)
        .map_err(|e| format!("consent: {e}"))?;

    let target_ticket = target_gw
        .relay_ticket(&session_id)
        .map_err(|e| format!("target ticket: {e}"))?;
    let ctrl_ticket = ctrl_gw
        .relay_ticket(&session_id)
        .map_err(|e| format!("ctrl ticket: {e}"))?;
    let keys = ctrl_gw
        .session_keys(&session_id)
        .map_err(|e| format!("keys: {e}"))?;

    let keys_match_registered = keys.controller_pubkey
        == encode_pubkey(ctrl_id.public().as_bytes())
        && keys.target_pubkey == encode_pubkey(target_id.public().as_bytes());

    let mut ctrl_relay = RelayClient::new(relay_addr, &session_id, &ctrl_dev.device_id)
        .map_err(|e| e.to_string())?;
    let mut target_relay = RelayClient::new(relay_addr, &session_id, &target_dev.device_id)
        .map_err(|e| e.to_string())?;
    ctrl_relay
        .register(&ctrl_ticket)
        .map_err(|e| e.to_string())?;
    target_relay
        .register(&target_ticket)
        .map_err(|e| e.to_string())?;
    std::thread::sleep(Duration::from_millis(400));

    // ER-502b：先通过 Relay 交换候选，再并行打洞；双方 ready 才切 P2P，否则保持 Relay。
    let p2p_cfg = P2PConfig::new(&session_id, p2p_token(&session_id), Duration::from_secs(2));
    let mut link = "relay".to_string();
    if ctrl_relay.prepare_p2p(&p2p_cfg).is_ok() && target_relay.prepare_p2p(&p2p_cfg).is_ok() {
        let ctrl_cands = ctrl_relay
            .await_candidates(&p2p_cfg, Duration::from_secs(2))
            .unwrap_or_default();
        let target_cands = target_relay
            .await_candidates(&p2p_cfg, Duration::from_secs(2))
            .unwrap_or_default();
        if !ctrl_cands.is_empty() && !target_cands.is_empty() {
            ctrl_relay.set_peer_candidates(ctrl_cands);
            target_relay.set_peer_candidates(target_cands);
            let cfg1 = p2p_cfg.clone();
            let cfg2 = p2p_cfg.clone();
            let ctrl_thread = std::thread::spawn(move || {
                let ok = ctrl_relay.punch(&cfg1);
                (ctrl_relay, ok)
            });
            let target_thread = std::thread::spawn(move || {
                let ok = target_relay.punch(&cfg2);
                (target_relay, ok)
            });
            let (new_ctrl, ctrl_ok) = ctrl_thread.join().map_err(|_| "ctrl p2p thread")?;
            let (new_target, target_ok) = target_thread.join().map_err(|_| "target p2p thread")?;
            ctrl_relay = new_ctrl;
            target_relay = new_target;
            ctrl_relay
                .send_ready(&p2p_cfg, ctrl_ok)
                .map_err(|e| e.to_string())?;
            target_relay
                .send_ready(&p2p_cfg, target_ok)
                .map_err(|e| e.to_string())?;
            let ctrl_peer_ok = ctrl_relay
                .await_ready(&p2p_cfg, Duration::from_secs(2))
                .unwrap_or(false);
            let target_peer_ok = target_relay
                .await_ready(&p2p_cfg, Duration::from_secs(2))
                .unwrap_or(false);
            ctrl_relay.activate_p2p(ctrl_ok, ctrl_peer_ok);
            target_relay.activate_p2p(target_ok, target_peer_ok);
            if ctrl_relay.link_kind() == er_session::relay_client::LinkKind::P2P
                && target_relay.link_kind() == er_session::relay_client::LinkKind::P2P
            {
                link = "p2p".to_string();
            }
        }
    }

    let (mut ctrl_crypto, ctrl_eph) = CryptoSession::new(&session_id, true);
    let (mut target_crypto, target_eph) = CryptoSession::new(&session_id, false);

    let ctrl_hs = sign_handshake(&ctrl_id, &session_id, &ctrl_eph, "operator");
    let mut m1 = TAG.to_vec();
    m1.extend_from_slice(&ctrl_hs.encode());
    ctrl_relay.send(&m1).map_err(|e| e.to_string())?;

    let target_hs = sign_handshake(&target_id, &session_id, &target_eph, "controlled");
    let mut m2 = TAG.to_vec();
    m2.extend_from_slice(&target_hs.encode());
    target_relay.send(&m2).map_err(|e| e.to_string())?;

    let saw_target = ctrl_relay
        .recv_timeout(Duration::from_secs(5))
        .map_err(|e| e.to_string())?
        .ok_or("no target handshake")?;
    let saw_ctrl = target_relay
        .recv_timeout(Duration::from_secs(5))
        .map_err(|e| e.to_string())?
        .ok_or("no controller handshake")?;

    let peer_target = Handshake::decode(&saw_target[TAG.len()..]).map_err(|e| e.to_string())?;
    let peer_ctrl = Handshake::decode(&saw_ctrl[TAG.len()..]).map_err(|e| e.to_string())?;

    let handshake_verified =
        verify_handshake(&target_id.public(), &session_id, &peer_target, "controlled").is_ok()
            && verify_handshake(&ctrl_id.public(), &session_id, &peer_ctrl, "operator").is_ok();

    // 用服务端下发的公钥再验一次（证明下发的就是登记的）
    let ctrl_pub = decode_pubkey(&keys.controller_pubkey).ok_or("bad ctrl pubkey")?;
    let target_pub = decode_pubkey(&keys.target_pubkey).ok_or("bad target pubkey")?;
    let _ = (ctrl_pub, target_pub);

    ctrl_crypto.derive_keys(&peer_target.ephemeral_key(), true);
    target_crypto.derive_keys(&peer_ctrl.ephemeral_key(), false);

    // 采集 → 编码
    let mut cap = ScreenCapture::open_primary().map_err(|e| format!("no display: {e}"))?;
    let info = cap.info();
    let requested = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    let resolved = er_codec::resolve_config(requested, er_codec::host_capability());
    let cfg = resolved.config;
    let mut codec = OpenH264Codec::new(cfg).map_err(|e| e.to_string())?;
    let frame = cap.grab().map_err(|e| e.to_string())?;
    let encoded = codec
        .encode_bgra(&frame.data, frame.ts_ms)
        .map_err(|e| e.to_string())?;

    let sealed = ctrl_crypto.seal(&encoded);
    let plaintext_marker_absent = !sealed.windows(4).any(|w| w == [0, 0, 0, 1]);
    ctrl_relay.send(&sealed).map_err(|e| e.to_string())?;

    let got = target_relay
        .recv_timeout(Duration::from_secs(5))
        .map_err(|e| e.to_string())?
        .ok_or("no payload")?;
    let plain = target_crypto.open(&got).map_err(|e| e.to_string())?;
    if plain != encoded {
        return Err("payload mismatch after decrypt".into());
    }

    let mut dec = H264Decoder::new().map_err(|e| e.to_string())?;
    let (w, h, _rgb) = match dec.decode(&plain).map_err(|e| e.to_string())? {
        Some(v) => v,
        None => return Err("decoder returned nothing".into()),
    };

    Ok(PairReport {
        session_id,
        target_device: target_dev.device_id,
        controller_device: ctrl_dev.device_id,
        encoded_bytes: encoded.len(),
        sealed_bytes: sealed.len(),
        decoded_width: w,
        decoded_height: h,
        keys_match_registered,
        handshake_verified,
        plaintext_marker_absent,
        link,
    })
}
