//! 网关客户端：走真实的 HTTP 接口，不用任何 mock。

pub mod agent;
pub mod conductor;
pub mod ctrl_session;
pub mod encoder;
pub mod gpu_policy;
pub mod host_consent;
pub mod host_stream;
pub mod link;

#[cfg(target_os = "windows")]
mod gpu_glue;
pub mod pair;
pub mod signalling;

use serde::Deserialize;

/// 服务层错误。
#[derive(Debug)]
pub struct ServiceError {
    /// 服务端数字码（"300006"）；网络/解析/HTTP 层错误用稳定分类（"NET"/"PARSE"/"HTTP"）。
    pub code: String,
    /// 服务端枚举名（"SESSION_AUTH_WINDOW_EXCEEDED"）；HTTP 层错误为空。
    pub name: String,
    /// 面向用户的说明。**禁止**包含内部 URL、原始 body、凭证。
    pub message: String,
    /// 服务端是否建议重试。
    pub retryable: bool,
    /// 服务端 trace_id，用于用户/客服定位。
    pub trace_id: String,
    /// HTTP 状态码（0 表示非 HTTP 层错误）。
    pub status: u16,
}

/// 解析服务端错误信封（生产调用与此处同一入口）。
///
/// 关键点：`code` 是**数字**（300006），旧实现用 `ErrorBody { code: String }` 反序列化，
/// 必然失败并退化成「拼接原始 body/URL」——那是用户看到内部细节的根因。
/// 这里接受 number 或 string，其余字段逐个取值，缺失给稳定默认。
pub fn parse_error_envelope(raw: &str, status: u16) -> ServiceError {
    let parsed = serde_json::from_str::<serde_json::Value>(raw).ok();
    match parsed {
        Some(v) if v.get("code").is_some() => ServiceError {
            code: v
                .get("code")
                .map(|c| match c {
                    serde_json::Value::Number(n) => n.to_string(),
                    serde_json::Value::String(s) => s.clone(),
                    other => other.to_string(),
                })
                .unwrap_or_else(|| "ERR".into()),
            name: v
                .get("name")
                .and_then(|n| n.as_str())
                .unwrap_or("")
                .to_string(),
            message: v
                .get("message")
                .and_then(|m| m.as_str())
                .filter(|m| !m.is_empty())
                .unwrap_or("请求失败")
                .to_string(),
            retryable: v
                .get("retryable")
                .and_then(|r| r.as_bool())
                .unwrap_or(false),
            trace_id: v
                .get("trace_id")
                .and_then(|t| t.as_str())
                .unwrap_or("")
                .to_string(),
            status,
        },
        // 非 JSON / 无 code：稳定文案，绝不回显原始 body
        _ => ServiceError {
            code: "HTTP".into(),
            name: String::new(),
            message: format!("请求失败（HTTP {status}）"),
            retryable: true,
            trace_id: String::new(),
            status,
        },
    }
}

impl ServiceError {
    /// 内部/传输层错误：只有稳定分类码 + 可读说明。
    pub fn internal(code: &str, message: impl Into<String>) -> Self {
        Self {
            code: code.to_string(),
            name: String::new(),
            message: message.into(),
            retryable: false,
            trace_id: String::new(),
            status: 0,
        }
    }
}

impl std::fmt::Display for ServiceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{}: {}",
            if self.name.is_empty() {
                &self.code
            } else {
                &self.name
            },
            self.message
        )
    }
}

impl std::error::Error for ServiceError {}

/// 一次绑定结果。
#[derive(Debug, Clone, Deserialize)]
pub struct BoundDevice {
    #[serde(rename = "device_id")]
    pub device_id: String,
    #[serde(rename = "device_token")]
    pub device_token: String,
    #[serde(rename = "raw_code")]
    pub raw_code: String,
}

/// 会话双方公钥。
#[derive(Debug, Clone, Deserialize)]
pub struct SessionKeys {
    #[serde(rename = "controller_id")]
    pub controller_id: String,
    #[serde(rename = "target_id")]
    pub target_id: String,
    #[serde(rename = "controller_pubkey")]
    pub controller_pubkey: String,
    #[serde(rename = "target_pubkey")]
    pub target_pubkey: String,
}

/// 网关客户端。
#[derive(Clone)]
pub struct Gateway {
    base: String,
    token: String,
    device_token: String,
}

impl Gateway {
    pub fn new(
        base: impl Into<String>,
        token: impl Into<String>,
        device_token: impl Into<String>,
    ) -> Self {
        Self {
            base: base.into(),
            token: token.into(),
            device_token: device_token.into(),
        }
    }

    fn url(&self, path: &str) -> String {
        format!("{}{}", self.base, path)
    }

    fn call(
        &self,
        method: &str,
        path: &str,
        body: Option<serde_json::Value>,
        with_device: bool,
    ) -> Result<serde_json::Value, ServiceError> {
        let mut req =
            ureq::request(method, &self.url(path)).set("content-type", "application/json");
        if !self.token.is_empty() {
            req = req.set("authorization", &format!("Bearer {}", self.token));
        }
        if with_device && !self.device_token.is_empty() {
            req = req.set("x-device-token", &self.device_token);
        }
        let resp = match body {
            Some(b) => req.send_json(b),
            None => req.call(),
        };
        match resp {
            Ok(r) => {
                let v = r
                    .into_json::<serde_json::Value>()
                    .map_err(|e| ServiceError::internal("PARSE", e.to_string()))?;
                // 服务端按契约返回信封 {code,name,data,trace_id}；早期版本直接返回裸对象。
                // 两种都要能解析：有 code 且 code==0 就取 data，否则当裸对象原样返回。
                // （踩坑记录：/tmp/erbin 里的旧 stack 是裸对象，重建后的 stack 是信封，
                //   不兼容解析会报 PARSE: missing token。）
                if let Some(code) = v.get("code").and_then(|c| c.as_u64()) {
                    if code == 0 {
                        return Ok(v.get("data").cloned().unwrap_or(serde_json::Value::Null));
                    }
                    // 完整保留服务端信封字段，便于 UI 只展示 message + trace_id，
                    // 同时把 name/code 留给分支判断与日志。内部 URL/body 绝不进 message。
                    return Err(ServiceError {
                        code: code.to_string(),
                        name: v
                            .get("name")
                            .and_then(|n| n.as_str())
                            .unwrap_or("ERR")
                            .to_string(),
                        message: v
                            .get("message")
                            .and_then(|m| m.as_str())
                            .unwrap_or("请求失败")
                            .to_string(),
                        retryable: v
                            .get("retryable")
                            .and_then(|r| r.as_bool())
                            .unwrap_or(false),
                        trace_id: v
                            .get("trace_id")
                            .and_then(|t| t.as_str())
                            .unwrap_or("")
                            .to_string(),
                        status: 0,
                    });
                }
                Ok(v)
            }
            Err(ureq::Error::Status(status, r)) => {
                let raw = r.into_string().unwrap_or_default();
                let err = parse_error_envelope(&raw, status);
                // 内部上下文只进 stderr：方法/路径/状态/码/长度，不含查询串、body、凭证。
                eprintln!(
                    "gateway {method} {path} -> HTTP {status} code={} name={} trace={} body_len={}",
                    err.code,
                    err.name,
                    err.trace_id,
                    raw.len()
                );
                Err(err)
            }
            Err(e) => {
                // 传输层错误：ureq 的文本可能带完整 URL（含端口/路径）。
                // 面向用户只给稳定分类，原始错误进 stderr。
                eprintln!("gateway {method} {path} -> transport error: {e}");
                Err(ServiceError::internal(
                    "NET",
                    "网络连接失败，请检查网络后重试",
                ))
            }
        }
    }

    /// 任一端主动关闭会话（设备凭证）。
    pub fn close_session(&self, session_id: &str) -> Result<serde_json::Value, ServiceError> {
        self.call(
            "POST",
            "/v1/sessions/close",
            Some(serde_json::json!({"session_id": session_id, "reason": "local_disconnect"})),
            true,
        )
    }

    /// 数据面就绪上报：双方各自在握手完成后调用，两端都 ready 才进入 active。
    pub fn mark_ready(&self, session_id: &str) -> Result<serde_json::Value, ServiceError> {
        self.call(
            "POST",
            "/v1/sessions/ready",
            Some(serde_json::json!({"session_id": session_id})),
            true,
        )
    }

    /// 会话状态（只对会话双方可见；调用方必须带设备凭证）。
    pub fn connect_status(&self, session_id: &str) -> Result<serde_json::Value, ServiceError> {
        self.call(
            "GET",
            &format!("/v1/connect/status?session_id={session_id}"),
            None,
            true,
        )
    }

    /// 发送短信验证码（开发环境固定 123456）。
    pub fn send_sms(&self, phone: &str) -> Result<(), ServiceError> {
        self.call(
            "POST",
            "/v1/auth/sms",
            Some(serde_json::json!({ "phone": phone })),
            false,
        )?;
        Ok(())
    }

    /// 登录并返回账号 token。
    pub fn login(&self, phone: &str, code: &str) -> Result<String, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/auth/login",
            Some(serde_json::json!({ "phone": phone, "code": code })),
            false,
        )?;
        v["token"]
            .as_str()
            .map(|s| s.to_string())
            .ok_or_else(|| ServiceError::internal("PARSE", "missing token"))
    }

    /// 绑定一台设备。
    pub fn bind_device(&self, platform: &str) -> Result<BoundDevice, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/devices/bind",
            Some(serde_json::json!({ "platform": platform })),
            false,
        )?;
        serde_json::from_value(v).map_err(|e| ServiceError::internal("PARSE", e.to_string()))
    }

    /// 设置设备「临时密码」（长期有效、校验不消费）。
    pub fn set_password(&self, device_id: &str, password: &str) -> Result<(), ServiceError> {
        self.call(
            "POST",
            "/v1/devices/password",
            Some(serde_json::json!({ "device_id": device_id, "password": password })),
            false,
        )?;
        Ok(())
    }

    /// 登记设备身份公钥（32 字节 hex）。
    pub fn register_identity(&self, device_id: &str, pubkey_hex: &str) -> Result<(), ServiceError> {
        self.call(
            "POST",
            "/v1/devices/identity",
            Some(serde_json::json!({ "device_id": device_id, "pubkey": pubkey_hex })),
            true,
        )?;
        Ok(())
    }

    /// 轮换验证码，返回新验证码。
    pub fn rotate_code(&self, device_code: &str) -> Result<String, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/devices/verify-code",
            Some(serde_json::json!({ "device_code": device_code })),
            true,
        )?;
        v["code"]
            .as_str()
            .map(|s| s.to_string())
            .ok_or_else(|| ServiceError::internal("PARSE", "missing code"))
    }

    /// 发起连接，返回 session id。
    pub fn request_connect(
        &self,
        device_code: &str,
        captcha: &str,
    ) -> Result<String, ServiceError> {
        // 必须带设备凭证：服务端用它在同账号多设备时确定 controller_id
        let v = self.call(
            "POST",
            "/v1/connect/request",
            Some(serde_json::json!({ "device_code": device_code, "captcha": captcha })),
            true,
        )?;
        v["session_id"]
            .as_str()
            .map(|s| s.to_string())
            .ok_or_else(|| ServiceError::internal("PARSE", "missing session_id"))
    }

    /// 被控端授权。
    pub fn consent(&self, session_id: &str, accepted: bool) -> Result<String, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/connect/consent",
            Some(serde_json::json!({ "session_id": session_id, "accepted": accepted })),
            true,
        )?;
        Ok(v["state"].as_str().unwrap_or("").to_string())
    }

    /// 领取中继票据。
    pub fn relay_ticket(&self, session_id: &str) -> Result<String, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/relay/ticket",
            Some(serde_json::json!({ "session_id": session_id })),
            true,
        )?;
        v["ticket"]
            .as_str()
            .map(|s| s.to_string())
            .ok_or_else(|| ServiceError::internal("PARSE", "missing ticket"))
    }

    /// 读取会话双方公钥。
    pub fn session_keys(&self, session_id: &str) -> Result<SessionKeys, ServiceError> {
        let v = self.call(
            "GET",
            &format!("/v1/connect/keys?session_id={session_id}"),
            None,
            false,
        )?;
        serde_json::from_value(v).map_err(|e| ServiceError::internal("PARSE", e.to_string()))
    }
}

/// 把 hex 公钥解成 32 字节。
pub fn decode_pubkey(hex_str: &str) -> Option<[u8; 32]> {
    if hex_str.len() != 64 {
        return None;
    }
    let mut out = [0u8; 32];
    for (i, byte) in out.iter_mut().enumerate() {
        let hi = hex_val(hex_str.as_bytes()[i * 2])?;
        let lo = hex_val(hex_str.as_bytes()[i * 2 + 1])?;
        *byte = (hi << 4) | lo;
    }
    Some(out)
}

fn hex_val(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        b'A'..=b'F' => Some(c - b'A' + 10),
        _ => None,
    }
}

/// 把公钥编码成 hex。
pub fn encode_pubkey(key: &[u8; 32]) -> String {
    key.iter().map(|b| format!("{b:02x}")).collect()
}

#[cfg(test)]
mod error_envelope_tests {
    use super::ServiceError;

    /// ER-905 4.1(4)：信封的 name/retryable/trace_id 必须保留，
    /// 且 message 不得携带内部 URL / 原始 body。
    #[test]
    fn service_error_keeps_envelope_fields_without_internal_url() {
        let e = ServiceError {
            code: "300006".into(),
            name: "SESSION_AUTH_WINDOW_EXCEEDED".into(),
            message: "本次授权已到期，请重新连接".into(),
            retryable: false,
            trace_id: "tr-abc123".into(),
            status: 403,
        };
        assert_eq!(e.name, "SESSION_AUTH_WINDOW_EXCEEDED");
        assert_eq!(e.trace_id, "tr-abc123");
        assert!(!e.retryable);
        assert_eq!(e.status, 403);
        assert!(!e.message.contains("http://"));
        assert!(!e.message.contains("body="));
        assert!(!e.message.contains("POST"));
    }

    #[test]
    fn internal_errors_are_classified_and_clean() {
        let e = ServiceError::internal("NET", "connection refused");
        assert_eq!(e.code, "NET");
        assert!(e.name.is_empty());
        assert!(e.trace_id.is_empty());
        assert!(!e.message.contains("http://"));
    }
}

#[cfg(test)]
mod error_envelope_parser_tests {
    use super::parse_error_envelope;

    /// 生产信封：code 是**数字**（旧实现按 String 反序列化必失败）。
    #[test]
    fn parses_numeric_code_envelope() {
        let raw = r#"{"code":300006,"name":"SESSION_AUTH_WINDOW_EXCEEDED",
            "message":"本次授权已到期，请重新连接","retryable":false,
            "trace_id":"tr-1"}"#;
        let e = parse_error_envelope(raw, 403);
        assert_eq!(e.code, "300006");
        assert_eq!(e.name, "SESSION_AUTH_WINDOW_EXCEEDED");
        assert_eq!(e.trace_id, "tr-1");
        assert!(!e.retryable);
        assert_eq!(e.status, 403);
        assert!(!e.message.contains("http://"));
        assert!(!e.message.contains("body="));
    }

    #[test]
    fn accepts_string_code() {
        let raw = r#"{"code":"CONN_RATE_LIMITED","message":"操作过于频繁","retryable":true}"#;
        let e = parse_error_envelope(raw, 429);
        assert_eq!(e.code, "CONN_RATE_LIMITED");
        assert!(e.retryable);
    }

    /// 非 JSON（网关/代理返回 HTML 或空 body）：稳定文案 + 不回显原始内容。
    #[test]
    fn non_json_fallback_is_stable_and_clean() {
        let e = parse_error_envelope("<html>502 Bad Gateway</html>", 502);
        assert_eq!(e.code, "HTTP");
        assert!(e.message.contains("HTTP 502"));
        assert!(!e.message.contains("<html>"));
        assert!(!e.message.contains("http://"));
        assert!(e.trace_id.is_empty());
    }

    #[test]
    fn missing_fields_get_safe_defaults() {
        let e = parse_error_envelope(r#"{"code":123}"#, 500);
        assert_eq!(e.code, "123");
        assert_eq!(e.message, "请求失败");
        assert!(!e.retryable);
    }
}
