//! 客户端信令：连接 rendezvous 的 WebSocket，接收「有人要连你」。
//!
//! 这一步补上了单端代理缺的那环：被控端不再需要别人告诉它会话 id，
//! 而是自己从信令里收到 CONSENT_REQUIRED，再返回 session_id。

use std::net::TcpStream;
use std::time::{Duration, Instant};

use tungstenite::stream::MaybeTlsStream;
use tungstenite::{connect, Message, WebSocket};

/// 信令消息类型（与 Go 端 `internal/signal` 保持一致）。
pub const TYPE_HELLO: u8 = 0x01;
pub const TYPE_HELLO_ACK: u8 = 0x02;
pub const TYPE_CONSENT_REQUIRED: u8 = 0x07;
pub const TYPE_DISCONNECT: u8 = 0x0C;

/// 客户端心跳（服务端未定义该类型，会忽略；作用是刷新 60s 读 deadline）。
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
}

impl Signalling {
    /// 连接并完成 HELLO 认证。
    pub fn connect(
        base_http: &str,
        device_id: &str,
        auth_token: &str,
        device_token: &str,
    ) -> Result<Self, SignalError> {
        let url = format!("{}/ws", base_http.replacen("http", "ws", 1));
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
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            if Instant::now() > deadline {
                return Err(SignalError("hello ack timeout".into()));
            }
            match ws.read() {
                Ok(Message::Text(txt)) => {
                    let v: serde_json::Value =
                        serde_json::from_str(&txt).map_err(|e| SignalError(e.to_string()))?;
                    match v["type"].as_u64().unwrap_or(0) as u8 {
                        TYPE_HELLO_ACK => return Ok(Self { ws }),
                        TYPE_DISCONNECT => {
                            return Err(SignalError(format!(
                                "unauthorized: {}",
                                v["body"]["reason"].as_str().unwrap_or("")
                            )))
                        }
                        _ => continue,
                    }
                }
                Ok(_) => continue,
                Err(e) => return Err(SignalError(format!("read: {e}"))),
            }
        }
    }

    /// 等待某个会话的授权请求，返回 session_id。
    ///
    /// 收到就立刻返回，不在这里做授权——调用方拿到 id 后再走同意流程，
    /// 便于 UI 弹确认框。
    ///
    /// **心跳**：服务端每收到一帧就把读 deadline 续到 +60s（stack/rendezvous.go 第 92 行），
    /// 而这里可能要等很久（被控端挂着等人来连）。不心跳的话 60s 后服务端主动断开，
    /// 表现是 Connection reset without closing handshake——实测被控端挂 1 分钟必掉。
    /// 因此把底层读超时设成 20s，超时就发一个未知类型的心跳帧（服务端 switch 不匹配会忽略）。
    pub fn wait_for_request(&mut self, timeout: Duration) -> Result<String, SignalError> {
        self.set_read_timeout(Some(Duration::from_secs(20)))?;
        let deadline = Instant::now() + timeout;
        loop {
            if Instant::now() > deadline {
                return Err(SignalError("consent request timeout".into()));
            }
            match self.ws.read() {
                Ok(Message::Text(txt)) => {
                    let v: serde_json::Value =
                        serde_json::from_str(&txt).map_err(|e| SignalError(e.to_string()))?;
                    if v["type"].as_u64().unwrap_or(0) as u8 == TYPE_CONSENT_REQUIRED {
                        let sid = v["body"]["session_id"].as_str().unwrap_or("").to_string();
                        if !sid.is_empty() {
                            return Ok(sid);
                        }
                    }
                }
                Ok(_) => continue,
                Err(tungstenite::Error::Io(e))
                    if e.kind() == std::io::ErrorKind::WouldBlock
                        || e.kind() == std::io::ErrorKind::TimedOut =>
                {
                    // 读超时：发心跳帧保活，继续等
                    let hb = serde_json::json!({ "type": TYPE_HEARTBEAT, "body": {} });
                    if let Err(e) = self.ws.send(Message::Text(hb.to_string())) {
                        return Err(SignalError(format!("heartbeat: {e}")));
                    }
                    continue;
                }
                Err(e) => return Err(SignalError(format!("read: {e}"))),
            }
        }
    }

    /// 设置底层 TCP 读超时（None = 阻塞）。
    fn set_read_timeout(&mut self, dur: Option<Duration>) -> Result<(), SignalError> {
        match self.ws.get_mut() {
            MaybeTlsStream::Plain(s) => s
                .set_read_timeout(dur)
                .map_err(|e| SignalError(format!("set_read_timeout: {e}"))),
            _ => Ok(()),
        }
    }
}
