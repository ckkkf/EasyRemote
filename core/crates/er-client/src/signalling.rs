//! 客户端信令：连接 rendezvous 的 WebSocket，接收「有人要连你」。
//!
//! 这一步补上了单端代理缺的那环：被控端不再需要别人告诉它会话 id，
//! 而是自己从信令里收到 CONSENT_REQUIRED，再返回 session_id。

use std::collections::VecDeque;
use std::net::TcpStream;
use std::time::{Duration, Instant};

use tungstenite::stream::MaybeTlsStream;
use tungstenite::{connect, Message, WebSocket};

/// 信令消息类型（与 Go 端 `internal/signal` 保持一致）。
pub const TYPE_HELLO: u8 = 0x01;
pub const TYPE_HELLO_ACK: u8 = 0x02;
pub const TYPE_CONSENT_REQUIRED: u8 = 0x07;
pub const TYPE_DISCONNECT: u8 = 0x0C;

/// 续租在线状态并刷新信令服务的读 deadline。
pub const TYPE_HEARTBEAT: u8 = 0x7F;

/// 信令错误。
#[derive(Debug)]
pub struct SignalError(pub String);

impl std::fmt::Display for SignalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl std::error::Error for SignalError {}

type Ws = WebSocket<MaybeTlsStream<TcpStream>>;

/// 信令客户端。
pub struct Signalling {
    ws: Ws,
    next_heartbeat: Instant,
    early_requests: VecDeque<String>,
}

impl Signalling {
    /// 连接并完成 HELLO 认证。
    pub fn connect(
        base_http: &str,
        device_id: &str,
        auth_token: &str,
        device_token: &str,
    ) -> Result<Self, SignalError> {
        let url = format!(
            "{}/ws",
            base_http.trim_end_matches('/').replacen("http", "ws", 1)
        );
        let (mut ws, _resp) = connect(&url).map_err(|e| SignalError(format!("ws connect: {e}")))?;

        let hello = serde_json::json!({
            "type": TYPE_HELLO,
            "body": {
                "device_id": device_id,
                "auth_token": auth_token,
                "device_token": device_token,
            }
        });
        ws.send(Message::Text(hello.to_string()))
            .map_err(|e| SignalError(format!("send hello: {e}")))?;

        // 必须等到 HELLO_ACK；收到 DISCONNECT 说明认证失败
        let mut signalling = Self {
            ws,
            next_heartbeat: Instant::now() + Duration::from_secs(15),
            early_requests: VecDeque::new(),
        };
        let hello_timeout = Duration::from_secs(5);
        signalling.set_read_timeout(Some(hello_timeout))?;
        let deadline = Instant::now() + hello_timeout;
        loop {
            if Instant::now() > deadline {
                return Err(SignalError("hello ack timeout".into()));
            }
            match signalling.ws.read() {
                Ok(Message::Text(txt)) => {
                    let v: serde_json::Value =
                        serde_json::from_str(&txt).map_err(|e| SignalError(e.to_string()))?;
                    match v["type"].as_u64().unwrap_or(0) as u8 {
                        TYPE_HELLO_ACK => return Ok(signalling),
                        TYPE_CONSENT_REQUIRED => {
                            // Presence can go live before the ACK reaches this socket.
                            // Keep such events, but do not deliver them until HELLO succeeds.
                            if let Some(sid) = consent_id(&v) {
                                const MAX_EARLY_REQUESTS: usize = 4;
                                if signalling.early_requests.len() >= MAX_EARLY_REQUESTS {
                                    return Err(SignalError("too many pre-ACK requests".into()));
                                }
                                signalling.early_requests.push_back(sid.to_string());
                            }
                        }
                        TYPE_DISCONNECT => {
                            return Err(SignalError(format!(
                                "unauthorized: {}",
                                v["body"]["reason"].as_str().unwrap_or("")
                            )))
                        }
                        _ => {}
                    }
                }
                Ok(_) => continue,
                Err(e) => return Err(SignalError(format!("read: {e}"))),
            }
        }
    }

    /// Wait for one authorization request; keep the connection (and lease)
    /// between requests. The desktop worker owns it for the lifetime of its host.
    pub fn wait_for_request(&mut self, timeout: Duration) -> Result<String, SignalError> {
        let deadline = Instant::now() + timeout;
        self.wait_while(|| Instant::now() < deadline)?
            .ok_or_else(|| SignalError("consent request timeout".into()))
    }

    pub(crate) fn wait_while(
        &mut self,
        running: impl Fn() -> bool,
    ) -> Result<Option<String>, SignalError> {
        const READ_SLICE: Duration = Duration::from_secs(1);
        const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(15);
        self.set_read_timeout(Some(READ_SLICE))?;
        while running() {
            if let Some(sid) = self.early_requests.pop_front() {
                return Ok(Some(sid));
            }
            if Instant::now() >= self.next_heartbeat {
                let heartbeat = serde_json::json!({ "type": TYPE_HEARTBEAT, "body": {} });
                self.ws
                    .send(Message::Text(heartbeat.to_string()))
                    .map_err(|e| SignalError(format!("heartbeat: {e}")))?;
                self.next_heartbeat = Instant::now() + HEARTBEAT_INTERVAL;
            }
            match self.ws.read() {
                Ok(Message::Text(txt)) => {
                    let v: serde_json::Value =
                        serde_json::from_str(&txt).map_err(|e| SignalError(e.to_string()))?;
                    if let Some(sid) = consent_id(&v) {
                        return Ok(Some(sid.to_string()));
                    }
                }
                Ok(Message::Close(_)) => return Err(SignalError("signal closed".into())),
                Ok(_) => {}
                Err(tungstenite::Error::Io(e))
                    if matches!(
                        e.kind(),
                        std::io::ErrorKind::WouldBlock | std::io::ErrorKind::TimedOut
                    ) => {}
                Err(e) => return Err(SignalError(format!("read: {e}"))),
            }
        }
        Ok(None)
    }

    /// 设置底层 TCP 读超时（None = 阻塞）。
    fn set_read_timeout(&mut self, dur: Option<Duration>) -> Result<(), SignalError> {
        let socket = match self.ws.get_mut() {
            MaybeTlsStream::Plain(socket) => socket,
            MaybeTlsStream::Rustls(tls) => &mut tls.sock,
            _ => return Err(SignalError("unsupported signaling transport".into())),
        };
        socket
            .set_read_timeout(dur)
            .and_then(|()| socket.set_write_timeout(dur))
            .map_err(|e| SignalError(format!("set socket timeout: {e}")))
    }
}

fn consent_id(message: &serde_json::Value) -> Option<&str> {
    if message["type"].as_u64()? != u64::from(TYPE_CONSENT_REQUIRED) {
        return None;
    }
    message["body"]["session_id"]
        .as_str()
        .filter(|sid| !sid.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{Ipv4Addr, TcpListener};

    #[test]
    fn preserves_consent_delivered_during_hello() {
        let listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let peer = std::thread::spawn(move || {
            let (tcp, _) = listener.accept().unwrap();
            let mut ws = tungstenite::accept(tcp).unwrap();
            assert!(ws.read().unwrap().is_text());
            for frame in [
                serde_json::json!({"type": TYPE_CONSENT_REQUIRED, "body":{"session_id":"first"}}),
                serde_json::json!({"type": TYPE_HELLO_ACK, "body":{}}),
            ] {
                ws.send(Message::Text(frame.to_string())).unwrap();
            }
        });
        let mut signal = Signalling::connect(&base, "device", "account", "token").unwrap();
        let result = signal.wait_for_request(Duration::from_secs(1));
        peer.join().unwrap();
        assert_eq!(result.unwrap(), "first");
    }
}
