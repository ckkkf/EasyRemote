//! 单端代理：主控与被控各自独立运行。
//!
//! 与 run_pair 的区别：run_pair 在一个函数里把两端串起来（便于自检），
//! 这里两端是**独立的会话对象**，可以分别跑在两个进程/两台机器上，
//! 这才是生产形态。

use std::net::SocketAddr;
use std::time::{Duration, Instant};

use crate::{decode_pubkey, encode_pubkey, BoundDevice, Gateway, SessionKeys};
use er_capture::{Capture, ScreenCapture};
use er_codec::{EncodeConfig, H264Decoder};
use er_session::crypto::CryptoSession;
use er_session::identity::{sign_handshake, verify_handshake, DeviceIdentity, Handshake};
use er_session::relay_client::RelayClient;

const TAG: &[u8] = b"ER-HS:";

/// 本机平台标识（绑设备时上报，真实产品里决定图标与能力）。
fn local_platform() -> &'static str {
    if cfg!(target_os = "windows") {
        "windows"
    } else if cfg!(target_os = "macos") {
        "macos"
    } else {
        "linux"
    }
}

/// 会话内消息类型（与分片重组后的载荷首字节对应）。
pub const MSG_VIDEO: u8 = 1;
pub const MSG_INPUT: u8 = 2;
/// 反向通道：接收侧 → 发送侧的链路反馈（ABR 的输入）
pub const MSG_FEEDBACK: u8 = 3;

/// 组一条视频消息：`[MSG_VIDEO][seq][ts][w][h][h264...]`。
///
/// 头部是丢帧率与 RTT 的唯一来源：没有 seq 就无法区分「网络丢包」和「静止桌面不出帧」。
pub fn encode_video_msg(hdr: &crate::link::VideoHeader, payload: &[u8]) -> Vec<u8> {
    let mut msg = Vec::with_capacity(1 + crate::link::VIDEO_HEADER_LEN + payload.len());
    msg.push(MSG_VIDEO);
    msg.extend_from_slice(&hdr.encode());
    msg.extend_from_slice(payload);
    msg
}

/// 一帧的接收结果。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FrameInfo {
    pub width: usize,
    pub height: usize,
    pub h264_bytes: usize,
}

/// 代理错误。
#[derive(Debug)]
pub struct AgentError(pub String);

impl std::fmt::Display for AgentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl std::error::Error for AgentError {}

impl From<String> for AgentError {
    fn from(s: String) -> Self {
        AgentError(s)
    }
}

macro_rules! fail {
    ($($arg:tt)*) => { return Err(AgentError(format!($($arg)*))) };
}

/// 被控端。
pub struct TargetAgent {
    gw: Gateway,
    device: BoundDevice,
    identity: DeviceIdentity,
    relay_addr: SocketAddr,
    token: String,
    gateway_base: String,
}

impl TargetAgent {
    /// 登录 + 绑设备 + 登记身份公钥。
    pub fn start(
        gateway_base: &str,
        phone: &str,
        sms_code: &str,
        relay_addr: SocketAddr,
    ) -> Result<Self, AgentError> {
        let anon = Gateway::new(gateway_base, "", "");
        let _ = anon.send_sms(phone);
        let token = anon
            .login(phone, sms_code)
            .map_err(|e| AgentError(e.to_string()))?;
        let gw = Gateway::new(gateway_base, &token, "");
        let device = gw
            .bind_device(local_platform())
            .map_err(|e| AgentError(e.to_string()))?;

        let identity = DeviceIdentity::random();
        let authed = Gateway::new(gateway_base, &token, &device.device_token);
        authed
            .register_identity(
                &device.device_id,
                &encode_pubkey(identity.public().as_bytes()),
            )
            .map_err(|e| AgentError(e.to_string()))?;

        Ok(Self {
            gw: authed,
            device,
            identity,
            relay_addr,
            token,
            gateway_base: gateway_base.to_string(),
        })
    }

    /// 通过信令等待「有人要连我」，返回 session_id。
    ///
    /// 这是生产形态的关键：被控端不再依赖外部告知会话 id。
    pub fn wait_for_session(
        &self,
        signal_base: &str,
        timeout: std::time::Duration,
    ) -> Result<String, AgentError> {
        let mut sig = crate::signalling::Signalling::connect(
            signal_base,
            &self.device.device_id,
            &self.token,
            &self.device.device_token,
        )
        .map_err(|e| AgentError(e.to_string()))?;
        sig.wait_for_request(timeout)
            .map_err(|e| AgentError(e.to_string()))
    }

    pub fn device_code(&self) -> String {
        self.device.raw_code.replace(' ', "")
    }

    pub fn device_id(&self) -> &str {
        &self.device.device_id
    }

    /// 轮换验证码并返回给主控（真实产品里是显示在屏幕上）。
    pub fn rotate_captcha(&self) -> Result<String, AgentError> {
        self.gw
            .rotate_code(&self.device_code())
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 授权会话。
    pub fn consent(&self, session_id: &str) -> Result<(), AgentError> {
        let state = self
            .gw
            .consent(session_id, true)
            .map_err(|e| AgentError(e.to_string()))?;
        if state != "authorized" {
            fail!("consent state = {}", state);
        }
        Ok(())
    }

    /// 握手第一步：注册中继并构造好本方握手消息（**不发送**）。
    /// 必须先让两端都注册，再发送，否则中继会把先到的包丢掉。
    pub fn begin(
        &self,
        session_id: &str,
    ) -> Result<(RelayClient, CryptoSession, Vec<u8>), AgentError> {
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        let keys = self
            .gw
            .session_keys(session_id)
            .map_err(|e| AgentError(e.to_string()))?;

        let mut relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
            .map_err(|e| AgentError(e.to_string()))?;
        relay
            .register(&ticket)
            .map_err(|e| AgentError(e.to_string()))?;

        let (crypto, eph) = CryptoSession::new(session_id, false);
        let hs = sign_handshake(&self.identity, session_id, &eph, "controlled");
        let mut msg = TAG.to_vec();
        msg.extend_from_slice(&hs.encode());
        let _ = keys;
        Ok((relay, crypto, msg))
    }

    /// 握手第二步：把本方握手消息发出去。
    pub fn announce(&self, relay: &mut RelayClient, msg: &[u8]) -> Result<(), AgentError> {
        relay
            .send(msg)
            .map(|_| ())
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 握手第二步：等待对端握手、验签、派生密钥。
    pub fn finish(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        session_id: &str,
    ) -> Result<(), AgentError> {
        let keys = self
            .gw
            .session_keys(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        // 中继是无序 UDP：握手可能重发、也可能晚到，这里循环等到一个能解出来的
        let peer = wait_peer_handshake(relay, Duration::from_secs(15))?;

        // 用服务端下发的公钥验签：中继无法伪造
        let ctrl_pub = decode_pubkey(&keys.controller_pubkey)
            .ok_or_else(|| AgentError("bad controller pubkey".into()))?;
        let verifier = ed25519_from_bytes(&ctrl_pub)?;
        verify_handshake(&verifier, session_id, &peer, "operator")
            .map_err(|e| AgentError(format!("controller signature invalid: {e}")))?;

        crypto.derive_keys(&peer.ephemeral_key(), false);
        Ok(())
    }

    /// 中继重启后重新注册（保留会话与密钥）。
    pub fn rejoin(&self, relay: &mut RelayClient, session_id: &str) -> Result<(), AgentError> {
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(format!("rejoin ticket: {e}")))?;
        relay
            .register(&ticket)
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 接收输入事件并注入。
    ///
    /// 返回注入的事件（便于测试断言）。视频/反馈消息会被跳过。
    ///
    /// **超时语义**：`timeout` 是整轮等待的总预算。中途某次 `recv_timeout` 空转
    /// （对端还在编码/发帧，输入事件晚到）**不能**当作失败——上一轮就是在这里
    /// panic `no input received` 的：静止桌面下被控端发完 1 帧就等输入，
    /// 而主控还在收帧，3s 空窗直接把整轮判死。
    pub fn recv_input(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        injector: &mut dyn er_input::Injector,
        count: usize,
        timeout: Duration,
    ) -> Result<Vec<er_input::InputEvent>, AgentError> {
        let mut got = Vec::with_capacity(count);
        let deadline = Instant::now() + timeout;
        while got.len() < count && Instant::now() < deadline {
            // 单次只等 500ms：既不会长时间卡住，也不会把「暂时没数据」判成失败
            let sealed = match relay.recv_timeout(Duration::from_millis(500)) {
                Ok(Some(s)) => s,
                Ok(None) => continue,
                Err(e) => return Err(AgentError(e.to_string())),
            };
            let plain = crypto
                .open(&sealed)
                .map_err(|e| AgentError(e.to_string()))?;
            if plain.is_empty() || plain[0] != MSG_INPUT {
                continue;
            }
            let Some(ev) = er_input::InputEvent::decode(&plain[1..]) else {
                continue;
            };
            injector
                .inject(&ev)
                .map_err(|e| AgentError(e.to_string()))?;
            got.push(ev);
        }
        if got.is_empty() {
            fail!("no input received");
        }
        Ok(got)
    }

    /// 发一份链路反馈（接收侧 → 发送侧）。返回实际发出的份数。
    pub fn send_feedback(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        report: &crate::link::FeedbackReport,
        copies: usize,
    ) -> Result<usize, AgentError> {
        send_feedback(relay, crypto, report, copies)
    }

    /// 等一份链路反馈（发送侧）。
    pub fn recv_feedback(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        timeout: Duration,
    ) -> Result<Option<crate::link::FeedbackReport>, AgentError> {
        recv_feedback(relay, crypto, timeout)
    }

    /// 接收并解码若干帧。
    pub fn recv_frames(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
    ) -> Result<Vec<FrameInfo>, AgentError> {
        recv_video_frames(relay, crypto, count)
    }

    /// 采集本机主屏并发送若干帧（被控侧共享自己屏幕）。返回每帧编码字节数。
    pub fn send_frames(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
    ) -> Result<Vec<usize>, AgentError> {
        send_screen_frames(relay, crypto, count)
    }
}

/// 接收并解码若干帧（两端共用）。
fn recv_video_frames(
    relay: &mut RelayClient,
    crypto: &mut CryptoSession,
    count: usize,
) -> Result<Vec<FrameInfo>, AgentError> {
    let mut out = Vec::with_capacity(count);
    let mut dec = H264Decoder::new().map_err(|e| AgentError(e.to_string()))?;
    let deadline = Instant::now() + Duration::from_secs(10);
    // 只解首帧：软件解码 1080p 约 90ms/帧，比 60fps 的到达速率慢，
    // 每帧都解会把 UDP 接收缓冲撑爆（实测「发 5 帧收 2 帧」）。
    // 后续帧只做完整性校验，尺寸沿用首帧。
    let mut dims: Option<(usize, usize)> = None;
    while out.len() < count && Instant::now() < deadline {
        let Some(sealed) = relay
            .recv_timeout(Duration::from_secs(3))
            .map_err(|e| AgentError(e.to_string()))?
        else {
            break;
        };
        // 握手副本晚到时是明文，解不出来；中继是无序 UDP，直接丢即可
        let plain = match crypto.open(&sealed) {
            Ok(p) => p,
            Err(_) if sealed.starts_with(TAG) => continue,
            Err(e) => fail!("decrypt: {e}"),
        };
        if plain.is_empty() {
            continue;
        }
        // 首字节是消息类型；输入事件与链路反馈由专门的接收路径处理
        if plain[0] != MSG_VIDEO {
            continue;
        }
        // 头部只为剥掉并（可选）记账：帧级丢帧率由调用方用 seq 统计
        let Some(_hdr) = crate::link::VideoHeader::decode(&plain[1..]) else {
            continue;
        };
        let plain = plain[1 + crate::link::VIDEO_HEADER_LEN..].to_vec();
        if dims.is_none() {
            match dec.decode(&plain).map_err(|e| AgentError(e.to_string()))? {
                Some((w, h, _)) => dims = Some((w, h)),
                None => continue,
            }
        }
        let (w, h) = dims.expect("dims set");
        out.push(FrameInfo {
            width: w,
            height: h,
            h264_bytes: plain.len(),
        });
    }
    if out.is_empty() {
        fail!("no frames decoded");
    }
    Ok(out)
}

/// 单调毫秒（RTT/时序用；只在本机内比较，无需跨机对齐）。
pub fn now_ms() -> u64 {
    use std::sync::OnceLock;
    use std::time::Instant;
    static START: OnceLock<Instant> = OnceLock::new();
    START.get_or_init(Instant::now).elapsed().as_millis() as u64
}

/// 发链路反馈：`copies` 份冗余 + 累积值（丢一份不影响收敛）。
fn send_feedback(
    relay: &mut RelayClient,
    crypto: &mut CryptoSession,
    report: &crate::link::FeedbackReport,
    copies: usize,
) -> Result<usize, AgentError> {
    let body = report.encode();
    let mut msg = Vec::with_capacity(1 + body.len());
    msg.push(MSG_FEEDBACK);
    msg.extend_from_slice(&body);
    let sealed = crypto.seal(&msg);
    let n = copies.max(1);
    for _ in 0..n {
        relay.send(&sealed).map_err(|e| AgentError(e.to_string()))?;
    }
    Ok(n)
}

/// 等一份链路反馈；超时返回 None。视频/输入消息被跳过。
fn recv_feedback(
    relay: &mut RelayClient,
    crypto: &mut CryptoSession,
    timeout: Duration,
) -> Result<Option<crate::link::FeedbackReport>, AgentError> {
    let deadline = Instant::now() + timeout;
    loop {
        let sealed = match relay.recv_timeout(Duration::from_millis(200)) {
            Ok(Some(s)) => s,
            Ok(None) => {
                if Instant::now() >= deadline {
                    return Ok(None);
                }
                continue;
            }
            Err(e) => return Err(AgentError(e.to_string())),
        };
        let plain = match crypto.open(&sealed) {
            Ok(p) => p,
            Err(_) => continue,
        };
        if plain.is_empty() || plain[0] != MSG_FEEDBACK {
            continue;
        }
        if let Some(r) = crate::link::FeedbackReport::decode(&plain[1..]) {
            return Ok(Some(r));
        }
    }
}

/// 采集主屏 → 编码 → 加密 → 中继发送（两端共用）。返回每帧编码字节数。
fn send_screen_frames(
    relay: &mut RelayClient,
    crypto: &mut CryptoSession,
    count: usize,
) -> Result<Vec<usize>, AgentError> {
    let mut cap =
        ScreenCapture::open_primary().map_err(|e| AgentError(format!("no display: {e}")))?;
    let info = cap.info();
    let cfg = EncodeConfig {
        width: info.width & !1,
        height: info.height & !1,
        fps: 60,
        bitrate_kbps: 4000,
        gop: 60,
        bframes: 0,
    };
    // 采集 + 编码：**Windows 上优先 DXGI→NVENC 零拷贝**，条件不满足自动回退 CPU
    // （回退原因由 HybridEncoder 打一行日志，只在切换时打一次）
    let mut enc = crate::encoder::HybridEncoder::new(cfg).map_err(AgentError)?;
    // 不要在这里打 backend：还没编过任何一帧，它必然是初始值 "cpu"，
    // 会让人误以为走了 CPU 路径（上一轮 step3 的误判就是这么来的）。
    println!(
        "CODEC: init kind={:?} {}x{}",
        enc.cpu_kind(),
        cfg.width,
        cfg.height
    );

    // 新订阅者/新会话：先制造一次画面变化，否则静止桌面下 DXGI 不出帧，
    // 观众会一直等不到首帧（零位移输入，不动光标）
    let primed = cap.prime_frame();
    println!("CODEC: prime_frame={primed}");
    let mut last_backend = enc.backend();

    let mut frame_seq: u32 = 0;
    let mut sizes = Vec::with_capacity(count);
    for _ in 0..count {
        let Some(encoded) = enc.capture_and_encode(&mut cap).map_err(AgentError)? else {
            // 静止桌面：没有新帧，本轮不发（上游可重复上一帧）
            continue;
        };
        // backend 变化时打一行（step3 断言靠它 grep）
        if enc.backend() != last_backend {
            last_backend = enc.backend();
            println!("CODEC: backend={}", last_backend);
        }
        if encoded.is_empty() {
            // 编码器没产出就不要发空消息：接收端会把 28 字节的空密文
            // 当成一帧交付，白白消耗接收窗口（这是丢帧的直接原因之一）。
            continue;
        }
        frame_seq = frame_seq.wrapping_add(1);
        let hdr = crate::link::VideoHeader {
            seq: frame_seq,
            ts_ms: now_ms() as u32,
            width: (cfg.width & 0xffff) as u16,
            height: (cfg.height & 0xffff) as u16,
        };
        let msg = encode_video_msg(&hdr, &encoded);
        let sealed = crypto.seal(&msg);
        relay.send(&sealed).map_err(|e| AgentError(e.to_string()))?;
        sizes.push(encoded.len());
        // 按 60fps 节流：不留间隔地灌帧会撑爆接收端 UDP 缓冲，
        // 表现为「发 5 帧只收 2 帧」而不是网络丢包。
        std::thread::sleep(Duration::from_millis(16));
    }
    println!("CODEC: backend_final={}", enc.backend());
    Ok(sizes)
}

/// 主控端。
pub struct ControllerAgent {
    gw: Gateway,
    device: BoundDevice,
    identity: DeviceIdentity,
    relay_addr: SocketAddr,
}

impl ControllerAgent {
    pub fn start(
        gateway_base: &str,
        phone: &str,
        sms_code: &str,
        relay_addr: SocketAddr,
    ) -> Result<Self, AgentError> {
        let anon = Gateway::new(gateway_base, "", "");
        let _ = anon.send_sms(phone);
        let token = anon
            .login(phone, sms_code)
            .map_err(|e| AgentError(e.to_string()))?;
        let gw = Gateway::new(gateway_base, &token, "");
        let device = gw
            .bind_device(local_platform())
            .map_err(|e| AgentError(e.to_string()))?;

        let identity = DeviceIdentity::random();
        let authed = Gateway::new(gateway_base, &token, &device.device_token);
        authed
            .register_identity(
                &device.device_id,
                &encode_pubkey(identity.public().as_bytes()),
            )
            .map_err(|e| AgentError(e.to_string()))?;

        Ok(Self {
            gw: authed,
            device,
            identity,
            relay_addr,
        })
    }

    pub fn device_id(&self) -> &str {
        &self.device.device_id
    }

    /// 用设备码 + 验证码发起连接。
    pub fn request_session(&self, device_code: &str, captcha: &str) -> Result<String, AgentError> {
        self.gw
            .request_connect(device_code, captcha)
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 握手第一步：注册中继并构造好本方握手消息（**不发送**）。
    pub fn begin(
        &self,
        session_id: &str,
    ) -> Result<(RelayClient, CryptoSession, SessionKeys, Vec<u8>), AgentError> {
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        let keys = self
            .gw
            .session_keys(session_id)
            .map_err(|e| AgentError(e.to_string()))?;

        let mut relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
            .map_err(|e| AgentError(e.to_string()))?;
        relay
            .register(&ticket)
            .map_err(|e| AgentError(e.to_string()))?;

        let (crypto, eph) = CryptoSession::new(session_id, true);
        let hs = sign_handshake(&self.identity, session_id, &eph, "operator");
        let mut msg = TAG.to_vec();
        msg.extend_from_slice(&hs.encode());
        Ok((relay, crypto, keys, msg))
    }

    /// 握手第二步：把本方握手消息发出去。
    pub fn announce(&self, relay: &mut RelayClient, msg: &[u8]) -> Result<(), AgentError> {
        relay
            .send(msg)
            .map(|_| ())
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 握手第二步：等待对端握手、验签、派生密钥。
    pub fn finish(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        keys: &SessionKeys,
        session_id: &str,
    ) -> Result<(), AgentError> {
        let peer = wait_peer_handshake(relay, Duration::from_secs(15))?;

        let target_pub = decode_pubkey(&keys.target_pubkey)
            .ok_or_else(|| AgentError("bad target pubkey".into()))?;
        let verifier = ed25519_from_bytes(&target_pub)?;
        verify_handshake(&verifier, session_id, &peer, "controlled")
            .map_err(|e| AgentError(format!("target signature invalid: {e}")))?;

        crypto.derive_keys(&peer.ephemeral_key(), true);
        Ok(())
    }

    /// 发送一批输入事件（控制方向：主控 → 被控）。
    pub fn send_input(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        events: &[er_input::InputEvent],
    ) -> Result<usize, AgentError> {
        for e in events {
            let body = e.encode();
            let mut msg = Vec::with_capacity(body.len() + 1);
            msg.push(MSG_INPUT);
            msg.extend_from_slice(&body);
            let sealed = crypto.seal(&msg);
            relay
                .send(&sealed)
                .map_err(|err| AgentError(err.to_string()))?;
        }
        Ok(events.len())
    }

    /// 中继重启后重新注册（**不重建加密会话**）。
    ///
    /// 会话级重连的关键：session_id 与密钥都保留，
    /// 只把端位补回去。接收端的 nonce 是单调递增的，因此可以无缝续传。
    pub fn rejoin(&self, relay: &mut RelayClient, session_id: &str) -> Result<(), AgentError> {
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(format!("rejoin ticket: {e}")))?;
        relay
            .register(&ticket)
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 采集本机主屏并发送若干帧（主控侧）。
    pub fn send_frames(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
    ) -> Result<Vec<usize>, AgentError> {
        send_screen_frames(relay, crypto, count)
    }

    /// 接收并解码被控端发来的帧（主控侧显示画面）。
    pub fn recv_frames(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
    ) -> Result<Vec<FrameInfo>, AgentError> {
        recv_video_frames(relay, crypto, count)
    }

    /// 发一份链路反馈（主控侧 = 视频接收侧，ABR 的测量点就是这里）。
    pub fn send_feedback(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        report: &crate::link::FeedbackReport,
        copies: usize,
    ) -> Result<usize, AgentError> {
        send_feedback(relay, crypto, report, copies)
    }

    /// 等一份链路反馈（被控侧作图/调试用）。
    pub fn recv_feedback(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        timeout: Duration,
    ) -> Result<Option<crate::link::FeedbackReport>, AgentError> {
        recv_feedback(relay, crypto, timeout)
    }
}

/// 等到一个可解析的对端握手（忽略重复副本与噪声）。
fn wait_peer_handshake(
    relay: &mut RelayClient,
    timeout: Duration,
) -> Result<Handshake, AgentError> {
    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        match relay.recv_timeout(Duration::from_secs(2)) {
            Ok(Some(msg)) => {
                if !msg.starts_with(TAG) {
                    continue;
                }
                match Handshake::decode(&msg[TAG.len()..]) {
                    Ok(h) => return Ok(h),
                    Err(_) => continue,
                }
            }
            Ok(None) => continue,
            Err(e) => fail!("relay recv: {e}"),
        }
    }
    fail!("no peer handshake within {:?}", timeout)
}

fn ed25519_from_bytes(bytes: &[u8; 32]) -> Result<ed25519_dalek::VerifyingKey, AgentError> {
    ed25519_dalek::VerifyingKey::from_bytes(bytes)
        .map_err(|e| AgentError(format!("bad verifying key: {e}")))
}
