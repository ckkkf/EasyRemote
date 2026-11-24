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

/// 主控心跳：`[MSG_HEARTBEAT]`，无负载。
///
/// 为什么需要它：UDP 中继不会通知"对端已离线"，被控端只能靠自己判断。
/// 没有心跳时只能靠 idle 超时（曾经 1800s）猜，主控走后被控端会空推半小时，
/// 这期间谁都连不进来。心跳让"对端还在不在"变成一个可测的事实。
pub const MSG_HEARTBEAT: u8 = 4;

/// 主控请求关键帧：[MSG_KEYFRAME]，无负载。
pub const MSG_KEYFRAME: u8 = 5;

/// 被控端输入 ACK：[MSG_INPUT_ACK][seq u32]（用于 P95 延迟统计）。
pub const MSG_INPUT_ACK: u8 = 6;

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
///
/// Clone 是给流式被控用的：prepare_host 把实例存进注册表，服务线程需要一份自己的句柄。
#[derive(Clone)]
pub struct TargetAgent {
    gw: Gateway,
    device: BoundDevice,
    identity: DeviceIdentity,
    relay_addr: SocketAddr,
    token: String,
}

impl TargetAgent {
    /// 用**已登录会话**构造被控端：不重新登录、不重新绑设备，设备码就是这台机器
    /// 在「我的设备」页里显示的那个。
    ///
    /// 为什么需要它：客户端已经是登录态，被控开关不该再走一遍短信登录（也没地方填码），
    /// 更不该每次启动都 bind 一台新设备（设备码会一直变）。
    pub fn from_session(
        gateway_base: &str,
        token: &str,
        device_id: &str,
        device_code: &str,
        device_token: &str,
        relay_addr: SocketAddr,
    ) -> Result<Self, AgentError> {
        if token.is_empty() || device_id.is_empty() {
            return Err(AgentError("missing session (token/device_id)".into()));
        }
        let device = BoundDevice {
            device_id: device_id.to_string(),
            device_token: device_token.to_string(),
            raw_code: device_code.to_string(),
        };
        let gw = Gateway::new(gateway_base, token, device_token);
        let identity = DeviceIdentity::random();
        gw.register_identity(device_id, &encode_pubkey(identity.public().as_bytes()))
            .map_err(|e| AgentError(e.to_string()))?;
        Ok(Self {
            gw,
            device,
            identity,
            relay_addr,
            token: token.to_string(),
        })
    }

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
        self.connect_signalling(signal_base)?
            .wait_for_request(timeout)
            .map_err(|e| AgentError(e.to_string()))
    }

    pub(crate) fn connect_signalling(
        &self,
        signal_base: &str,
    ) -> Result<crate::signalling::Signalling, AgentError> {
        crate::signalling::Signalling::connect(
            signal_base,
            &self.device.device_id,
            &self.token,
            &self.device.device_token,
        )
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

    /// 设置本设备的「临时密码」（被控端本地生成，服务端只存哈希）。
    pub fn set_password(&self, password: &str) -> Result<(), AgentError> {
        self.gw
            .set_password(&self.device.device_id, password)
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 读取服务端会话状态（关闭传播的兜底观察点）。
    pub fn session_state(&self, session_id: &str) -> Result<String, AgentError> {
        let v = self
            .gw
            .connect_status(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        Ok(v.get("state")
            .and_then(|s| s.as_str())
            .unwrap_or("")
            .to_string())
    }

    /// 本机断开当前会话（被控端本地控制条）。
    pub fn close_session(&self, session_id: &str) -> Result<(), AgentError> {
        self.gw
            .close_session(session_id)
            .map(|_| ())
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 数据面就绪上报（被控端）。
    /// 结构化版 mark_ready：保留 ServiceError 的 code/name/message/retryable/trace_id。
    ///
    /// 为什么需要 _ex 变体：ER-905 要求 attach 失败时 UI 能拿到 trace_id，
    /// 而 AgentError 只是 String，会把结构化字段压平（再靠字符串猜 HTTP 是错的）。
    pub fn mark_ready_ex(&self, session_id: &str) -> Result<(), crate::ServiceError> {
        self.gw.mark_ready(session_id).map(|_| ())
    }

    pub fn mark_ready(&self, session_id: &str) -> Result<(), AgentError> {
        self.gw
            .mark_ready(session_id)
            .map(|_| ())
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 拒绝会话（本机授权弹窗选择拒绝/超时）。
    pub fn reject(&self, session_id: &str) -> Result<(), AgentError> {
        self.gw
            .consent(session_id, false)
            .map(|_| ())
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

    /// 结构化版 begin：网关卡点的错误保留 code/name/retryable/trace_id。
    ///
    /// relay_ticket 的 403 就发生在这里——正是它带着 trace_id，
    /// 走 AgentError 会被压成字符串，UI 再也拿不到。
    pub fn begin_ex(
        &self,
        session_id: &str,
    ) -> Result<(RelayClient, CryptoSession, Vec<u8>), crate::ServiceError> {
        let ticket = self.gw.relay_ticket(session_id)?;
        let keys = self.gw.session_keys(session_id)?;

        let relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
            .map_err(|e| crate::ServiceError::internal("RELAY", e.to_string()))?;
        relay
            .register(&ticket)
            .map_err(|e| crate::ServiceError::internal("RELAY", e.to_string()))?;

        let (crypto, eph) = CryptoSession::new(session_id, false);
        let hs = sign_handshake(&self.identity, session_id, &eph, "controlled");
        let mut msg = TAG.to_vec();
        msg.extend_from_slice(&hs.encode());
        let _ = keys;
        Ok((relay, crypto, msg))
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

        let relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
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
        self.finish_with_resend(relay, crypto, session_id, None)
    }

    /// 与 finish 相同，但在等待对端期间周期重发自己的握手包。
    ///
    /// 中继是无序 UDP 且注册与数据包没有确认：两端同时注册时，先发的握手包
    /// 可能在对端注册完成前被丢弃，只靠一次发送会出现「双方都在等对方」。
    pub fn finish_with_resend(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        session_id: &str,
        own_msg: Option<&[u8]>,
    ) -> Result<(), AgentError> {
        let keys = self
            .gw
            .session_keys(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(format!("finish ticket: {e}")))?;
        // 中继没有注册确认：等待期间同时重发注册与握手，避免「注册包丢了一次就双方干等」。
        let peer = wait_peer_handshake(relay, Duration::from_secs(15), own_msg, Some(&ticket))?;
        // 对端注册前发出的握手会被中继丢弃。收到对端握手说明它已注册，
        // 这里再补发一次自己的，保证对方也能收到（否则双方会一方完成、一方干等）。
        if let Some(msg) = own_msg {
            let _ = relay.send(msg);
        }

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
    let mut file_rx: Option<er_channels::file_stream::StreamReceiver> = None;
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
        // ER-711d：文件通道（opt-in，仅目标侧配置了接收目录时处理）。
        if plain[0] == er_channels::file_stream::MSG_FILE_META
            && std::env::var("ER_FILE_RECV_DIR").is_ok()
        {
            file_rx = er_channels::file_stream::StreamReceiver::from_meta(&plain);
            continue;
        }
        if plain[0] == er_channels::file_stream::MSG_FILE_CHUNK {
            if let Some(rx) = file_rx.as_mut() {
                let id = rx.id();
                if let Ok((index, _)) = rx.on_chunk(&plain) {
                    let complete = rx.missing().is_empty();
                    let name = rx.name().to_string();
                    if !complete {
                        let ack = er_channels::file_stream::StreamReceiver::ack(index, id);
                        let sealed = crypto.seal(&ack);
                        let _ = relay.send(&sealed);
                    } else {
                        let dir = std::env::var("ER_FILE_RECV_DIR").unwrap_or_else(|_| ".".into());
                        let file_name = std::path::Path::new(&name)
                            .file_name()
                            .and_then(|s| s.to_str())
                            .unwrap_or("file.bin");
                        let path = std::path::Path::new(&dir).join(file_name);
                        let stored = file_rx.take().is_some_and(|done| {
                            done.finish()
                                .ok()
                                .is_some_and(|bytes| std::fs::write(&path, &bytes).is_ok())
                        });
                        if stored {
                            let ack = er_channels::file_stream::StreamReceiver::ack(index, id);
                            let sealed = crypto.seal(&ack);
                            let _ = relay.send(&sealed);
                        }
                    }
                }
            }
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

/// 接收若干帧，返回每帧的 H.264 裸码流（Annex-B）。
fn recv_video_raw(
    relay: &mut RelayClient,
    crypto: &mut CryptoSession,
    count: usize,
    timeout: Duration,
) -> Result<Vec<Vec<u8>>, AgentError> {
    let mut out: Vec<Vec<u8>> = Vec::with_capacity(count);
    let deadline = Instant::now() + timeout;
    while out.len() < count && Instant::now() < deadline {
        let Some(sealed) = relay
            .recv_timeout(Duration::from_secs(3))
            .map_err(|e| AgentError(e.to_string()))?
        else {
            continue;
        };
        let plain = match crypto.open(&sealed) {
            Ok(p) => p,
            Err(_) if sealed.starts_with(TAG) => continue,
            Err(e) => fail!("decrypt: {e}"),
        };
        if plain.is_empty() || plain[0] != MSG_VIDEO {
            continue;
        }
        let Some(_hdr) = crate::link::VideoHeader::decode(&plain[1..]) else {
            continue;
        };
        let payload = &plain[1 + crate::link::VIDEO_HEADER_LEN..];
        if payload.is_empty() {
            continue;
        }
        out.push(payload.to_vec());
    }
    if out.is_empty() {
        fail!("no frames received");
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
    input_seq: std::sync::atomic::AtomicU32,
}

impl ControllerAgent {
    /// 用已登录会话构造主控端（不重新登录、不重新请求会话）。
    ///
    /// 客户端已经在 GatewayClient 里建过会话，FFI 只需要"接管"它跑收发；
    /// 再 request 一次会撞上「设备正在被控制」。
    pub fn from_session(
        gateway_base: &str,
        token: &str,
        device_id: &str,
        device_token: &str,
        relay_addr: SocketAddr,
    ) -> Result<Self, AgentError> {
        if token.is_empty() || device_id.is_empty() {
            return Err(AgentError("missing session (token/device_id)".into()));
        }
        let device = BoundDevice {
            device_id: device_id.to_string(),
            device_token: device_token.to_string(),
            raw_code: String::new(),
        };
        let gw = Gateway::new(gateway_base, token, device_token);
        let identity = DeviceIdentity::random();
        gw.register_identity(device_id, &encode_pubkey(identity.public().as_bytes()))
            .map_err(|e| AgentError(e.to_string()))?;
        Ok(Self {
            gw,
            device,
            identity,
            relay_addr,
            input_seq: std::sync::atomic::AtomicU32::new(0),
        })
    }

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
            input_seq: std::sync::atomic::AtomicU32::new(0),
        })
    }

    pub fn device_id(&self) -> &str {
        &self.device.device_id
    }

    /// 用设备码 + 验证码发起连接。
    /// 读取服务端会话状态（关闭传播的兜底观察点）。
    pub fn session_state(&self, session_id: &str) -> Result<String, AgentError> {
        let v = self
            .gw
            .connect_status(session_id)
            .map_err(|e| AgentError(e.to_string()))?;
        Ok(v.get("state")
            .and_then(|s| s.as_str())
            .unwrap_or("")
            .to_string())
    }

    /// 数据面就绪上报（主控端）。
    /// 结构化版 mark_ready（主控）：保留 ServiceError 全部字段。
    pub fn mark_ready_ex(&self, session_id: &str) -> Result<(), crate::ServiceError> {
        self.gw.mark_ready(session_id).map(|_| ())
    }

    pub fn mark_ready(&self, session_id: &str) -> Result<(), AgentError> {
        self.gw
            .mark_ready(session_id)
            .map(|_| ())
            .map_err(|e| AgentError(e.to_string()))
    }

    pub fn request_session(&self, device_code: &str, captcha: &str) -> Result<String, AgentError> {
        self.gw
            .request_connect(device_code, captcha)
            .map_err(|e| AgentError(e.to_string()))
    }

    /// 等会话真正进入 authorized（被控端已同意）再取票据。
    ///
    /// HTTP 成功只代表「请求已登记」，不代表对端已同意；直接取票据会拿到 403。
    /// 这是 ER-201（事件驱动）落地前的有界轮询兜底，不能让 UI 无限等待。
    pub fn wait_authorized(
        &self,
        session_id: &str,
        timeout: std::time::Duration,
    ) -> Result<(), AgentError> {
        let deadline = std::time::Instant::now() + timeout;
        loop {
            let v = self
                .gw
                .connect_status(session_id)
                .map_err(|e| AgentError(e.to_string()))?;
            let state = v.get("state").and_then(|s| s.as_str()).unwrap_or("");
            match state {
                "authorized" | "active" => return Ok(()),
                "failed" | "closed" | "ended" => {
                    return Err(AgentError(format!(
                        "session {state}: {}",
                        v.get("reason").and_then(|r| r.as_str()).unwrap_or("")
                    )))
                }
                _ => {}
            }
            if std::time::Instant::now() >= deadline {
                return Err(AgentError(format!(
                    "authorization timeout (last state={state})"
                )));
            }
            std::thread::sleep(std::time::Duration::from_millis(50));
        }
    }

    /// 结构化版 begin（主控）：relay_ticket 的 403 在这里，必须保留 trace_id。
    pub fn begin_ex(
        &self,
        session_id: &str,
    ) -> Result<(RelayClient, CryptoSession, SessionKeys, Vec<u8>), crate::ServiceError> {
        let ticket = self.gw.relay_ticket(session_id)?;
        let keys = self.gw.session_keys(session_id)?;

        let relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
            .map_err(|e| crate::ServiceError::internal("RELAY", e.to_string()))?;
        relay
            .register(&ticket)
            .map_err(|e| crate::ServiceError::internal("RELAY", e.to_string()))?;

        let (crypto, eph) = CryptoSession::new(session_id, true);
        let hs = sign_handshake(&self.identity, session_id, &eph, "operator");
        let mut msg = TAG.to_vec();
        msg.extend_from_slice(&hs.encode());
        let _ = keys;
        Ok((relay, crypto, keys, msg))
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

        let relay = RelayClient::new(self.relay_addr, session_id, &self.device.device_id)
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
        self.finish_with_resend(relay, crypto, keys, session_id, None)
    }

    /// 与 finish 相同，但等待对端期间周期重发自己的握手包（见 TargetAgent 同名方法）。
    /// 结构化版 finish：`relay_ticket` 是第三个网关卡点，失败也要保留 trace_id。
    ///
    /// 本地中继/验签失败走 `ServiceError::internal` + 稳定脱敏文案，
    /// 原文（可能含 URL/路径）只进 eprintln。
    pub fn finish_ex(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        keys: &SessionKeys,
        session_id: &str,
        own_msg: Option<&[u8]>,
    ) -> Result<(), crate::ServiceError> {
        let ticket = self.gw.relay_ticket(session_id)?;
        let peer = wait_peer_handshake(relay, Duration::from_secs(15), own_msg, Some(&ticket))
            .map_err(|e| {
                eprintln!("attach finish handshake error: {e}");
                crate::ServiceError::internal("HANDSHAKE", "建立加密通道超时，请确认对方已开启被控")
            })?;
        if let Some(msg) = own_msg {
            let _ = relay.send(msg);
        }
        let target_pub = decode_pubkey(&keys.target_pubkey).ok_or_else(|| {
            crate::ServiceError::internal("HANDSHAKE", "对方公钥无效，请重新连接")
        })?;
        let verifier = ed25519_from_bytes(&target_pub).map_err(|e| {
            eprintln!("attach verify key error: {e}");
            crate::ServiceError::internal("HANDSHAKE", "对方公钥无效，请重新连接")
        })?;
        verify_handshake(&verifier, session_id, &peer, "controlled").map_err(|e| {
            eprintln!("attach verify handshake error: {e}");
            crate::ServiceError::internal("HANDSHAKE", "对方身份校验失败，请重新连接")
        })?;
        crypto.derive_keys(&peer.ephemeral_key(), true);
        Ok(())
    }

    pub fn finish_with_resend(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        keys: &SessionKeys,
        session_id: &str,
        own_msg: Option<&[u8]>,
    ) -> Result<(), AgentError> {
        let ticket = self
            .gw
            .relay_ticket(session_id)
            .map_err(|e| AgentError(format!("finish ticket: {e}")))?;
        let peer = wait_peer_handshake(relay, Duration::from_secs(15), own_msg, Some(&ticket))?;
        // 同 TargetAgent：收到对端后补发一次，避免自己的包在对方注册前被丢弃。
        if let Some(msg) = own_msg {
            let _ = relay.send(msg);
        }

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
    ) -> Result<u32, AgentError> {
        let mut first_seq = 0;
        for (i, e) in events.iter().enumerate() {
            let seq = self
                .input_seq
                .fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            if i == 0 {
                first_seq = seq;
            }
            let body = e.encode();
            let mut msg = Vec::with_capacity(body.len() + 5);
            msg.push(MSG_INPUT);
            msg.extend_from_slice(&seq.to_be_bytes());
            msg.extend_from_slice(&body);
            let sealed = crypto.seal(&msg);
            relay
                .send(&sealed)
                .map_err(|err| AgentError(err.to_string()))?;
        }
        Ok(first_seq)
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

    /// 接收若干帧并返回 **H.264 原始字节**（不解码）。用于录制/离线分析。
    ///
    /// 为什么不解码：软件解码 1080p 约 90ms/帧，边收边解会把 UDP 接收缓冲撑爆；
    /// 而录制只关心码流完整，落盘后交给 ffmpeg 处理才是正确分工。
    pub fn recv_video_raw(
        &self,
        relay: &mut RelayClient,
        crypto: &mut CryptoSession,
        count: usize,
        timeout: Duration,
    ) -> Result<Vec<Vec<u8>>, AgentError> {
        recv_video_raw(relay, crypto, count, timeout)
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
    resend: Option<&[u8]>,
    ticket: Option<&str>,
) -> Result<Handshake, AgentError> {
    let deadline = Instant::now() + timeout;
    let mut next_resend = Instant::now();
    while Instant::now() < deadline {
        if Instant::now() >= next_resend {
            // 注册与握手一起重发：中继注册没有 ACK，丢一次就永远收不到对端。
            if let Some(t) = ticket {
                let _ = relay.register(t);
            }
            if let Some(msg) = resend {
                let _ = relay.send(msg);
            }
            next_resend = Instant::now() + Duration::from_millis(300);
        }
        match relay.recv_timeout(Duration::from_millis(300)) {
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
