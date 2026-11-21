//! 网关客户端：走真实的 HTTP 接口，不用任何 mock。

pub mod agent;
pub mod conductor;
pub mod encoder;
pub mod ctrl_session;
pub mod host_stream;
pub mod gpu_policy;
pub mod link;

#[cfg(target_os = "windows")]
mod gpu_glue;
pub mod pair;
pub mod signalling;

use serde::Deserialize;

/// 服务层错误。
#[derive(Debug)]
pub struct ServiceError {
    pub code: String,
    pub message: String,
}

impl std::fmt::Display for ServiceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}

impl std::error::Error for ServiceError {}

#[derive(Deserialize)]
struct ErrorBody {
    code: String,
    message: String,
}

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
                let v = r.into_json::<serde_json::Value>().map_err(|e| ServiceError {
                    code: "PARSE".into(),
                    message: e.to_string(),
                })?;
                // 服务端按契约返回信封 {code,name,data,trace_id}；早期版本直接返回裸对象。
                // 两种都要能解析：有 code 且 code==0 就取 data，否则当裸对象原样返回。
                // （踩坑记录：/tmp/erbin 里的旧 stack 是裸对象，重建后的 stack 是信封，
                //   不兼容解析会报 PARSE: missing token。）
                if let Some(code) = v.get("code").and_then(|c| c.as_u64()) {
                    if code == 0 {
                        return Ok(v.get("data").cloned().unwrap_or(serde_json::Value::Null));
                    }
                    return Err(ServiceError {
                        code: v
                            .get("name")
                            .and_then(|n| n.as_str())
                            .unwrap_or("ERR")
                            .to_string(),
                        message: v
                            .get("message")
                            .and_then(|m| m.as_str())
                            .unwrap_or("request failed")
                            .to_string(),
                    });
                }
                Ok(v)
            }
            Err(ureq::Error::Status(_, r)) => {
                let err = r.into_json::<ErrorBody>().unwrap_or(ErrorBody {
                    code: "HTTP".into(),
                    message: "request failed".into(),
                });
                Err(ServiceError {
                    code: err.code,
                    message: err.message,
                })
            }
            Err(e) => Err(ServiceError {
                code: "NET".into(),
                message: e.to_string(),
            }),
        }
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
            .ok_or_else(|| ServiceError {
                code: "PARSE".into(),
                message: "missing token".into(),
            })
    }

    /// 绑定一台设备。
    pub fn bind_device(&self, platform: &str) -> Result<BoundDevice, ServiceError> {
        let v = self.call(
            "POST",
            "/v1/devices/bind",
            Some(serde_json::json!({ "platform": platform })),
            false,
        )?;
        serde_json::from_value(v).map_err(|e| ServiceError {
            code: "PARSE".into(),
            message: e.to_string(),
        })
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
            .ok_or_else(|| ServiceError {
                code: "PARSE".into(),
                message: "missing code".into(),
            })
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
            .ok_or_else(|| ServiceError {
                code: "PARSE".into(),
                message: "missing session_id".into(),
            })
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
            .ok_or_else(|| ServiceError {
                code: "PARSE".into(),
                message: "missing ticket".into(),
            })
    }

    /// 读取会话双方公钥。
    pub fn session_keys(&self, session_id: &str) -> Result<SessionKeys, ServiceError> {
        let v = self.call(
            "GET",
            &format!("/v1/connect/keys?session_id={session_id}"),
            None,
            false,
        )?;
        serde_json::from_value(v).map_err(|e| ServiceError {
            code: "PARSE".into(),
            message: e.to_string(),
        })
    }
}

/// 把 hex 公钥解成 32 字节。
pub fn decode_pubkey(hex_str: &str) -> Option<[u8; 32]> {
    if hex_str.len() != 64 {
        return None;
    }
    let mut out = [0u8; 32];
    for i in 0..32 {
        let hi = hex_val(hex_str.as_bytes()[i * 2])?;
        let lo = hex_val(hex_str.as_bytes()[i * 2 + 1])?;
        out[i] = (hi << 4) | lo;
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
