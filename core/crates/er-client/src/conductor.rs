//! 会话编排：主机侧与被控侧的真实两段式流程。
//!
//! v2 变化：主控不再需要 host_id（进程内知识），而是用**设备码 + 验证码**
//! 连接——这与真实产品一致。被控端通过信令自行发现会话。

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::{Mutex, OnceLock};

use crate::agent::{ControllerAgent, FrameInfo, TargetAgent};

/// 已准备好的主机（已登录、绑设备、登记身份、轮换了验证码）。
pub struct PreparedHost {
    pub host_id: String,
    pub device_code: String,
    pub captcha: String,
    target: TargetAgent,
    relay_addr: SocketAddr,
}

/// 主机侧接收结果。
#[derive(Debug, Clone)]
pub struct HostResult {
    pub session_id: String,
    pub frames: Vec<FrameInfo>,
}

fn registry() -> &'static Mutex<HashMap<String, PreparedHost>> {
    static REG: OnceLock<Mutex<HashMap<String, PreparedHost>>> = OnceLock::new();
    REG.get_or_init(|| Mutex::new(HashMap::new()))
}

/// 准备主机：返回 host_id、设备码、验证码（真实产品里显示在屏幕上）。
pub fn prepare_host(
    gateway: &str,
    relay: SocketAddr,
    phone: &str,
    sms_code: &str,
) -> Result<(String, String, String), String> {
    let target = TargetAgent::start(gateway, phone, sms_code, relay).map_err(|e| e.to_string())?;
    let code = target.device_code();
    let captcha = target.rotate_captcha().map_err(|e| e.to_string())?;
    let host_id = format!("host-{}", nanos_hex());
    registry()
        .lock()
        .map_err(|_| "registry poisoned".to_string())?
        .insert(
            host_id.clone(),
            PreparedHost {
                host_id: host_id.clone(),
                device_code: code.clone(),
                captcha: captcha.clone(),
                target,
                relay_addr: relay,
            },
        );
    Ok((host_id, code, captcha))
}

/// 主机侧服务：等信令通知 → 授权 → 握手 → 收帧。**阻塞调用**，应在后台 isolate 跑。
pub fn serve_host(
    host_id: &str,
    signal_base: &str,
    frames: usize,
    timeout_secs: u64,
) -> Result<HostResult, String> {
    let (target, relay_addr) = {
        let reg = registry()
            .lock()
            .map_err(|_| "registry poisoned".to_string())?;
        let h = reg
            .get(host_id)
            .ok_or_else(|| format!("unknown host: {host_id}"))?;
        // TargetAgent 不可 Clone，这里用一个轻量句柄：把需要的东西先取出来
        (h.device_code.clone(), ())
    };
    let _ = (target, relay_addr);

    // 真正的等待与收帧要在持有 target 的情况下做，因此整个流程放在注册表锁内不合理，
    // 改为「取出再放回」：取走对象、执行、丢弃。
    let host = {
        let mut reg = registry()
            .lock()
            .map_err(|_| "registry poisoned".to_string())?;
        reg.remove(host_id)
            .ok_or_else(|| format!("unknown host: {host_id}"))?
    };

    let session_id = host
        .target
        .wait_for_session(signal_base, std::time::Duration::from_secs(timeout_secs))
        .map_err(|e| e.to_string())?;

    host.target
        .consent(&session_id)
        .map_err(|e| e.to_string())?;
    let (mut t_relay, mut t_crypto, t_msg) =
        host.target.begin(&session_id).map_err(|e| e.to_string())?;
    host.target
        .announce(&mut t_relay, &t_msg)
        .map_err(|e| e.to_string())?;
    host.target
        .finish_with_resend(&mut t_relay, &mut t_crypto, &session_id, Some(&t_msg))
        .map_err(|e| e.to_string())?;
    host.target
        .mark_ready(&session_id)
        .map_err(|e| e.to_string())?;

    let frames_got = host
        .target
        .recv_frames(&mut t_relay, &mut t_crypto, frames)
        .map_err(|e| e.to_string())?;

    Ok(HostResult {
        session_id,
        frames: frames_got,
    })
}

/// 主控侧连接：只用设备码 + 验证码（与真实产品一致）。
pub fn connect_device(
    gateway: &str,
    relay: SocketAddr,
    controller_phone: &str,
    device_code: &str,
    captcha: &str,
    sms_code: &str,
    frames: usize,
) -> Result<HostResult, String> {
    let ctrl = ControllerAgent::start(gateway, controller_phone, sms_code, relay)
        .map_err(|e| e.to_string())?;
    let session_id = ctrl
        .request_session(device_code, captcha)
        .map_err(|e| e.to_string())?;
    // 请求成功 ≠ 对端已同意：等 authorized 再取票据，避免 403 竞态。
    ctrl.wait_authorized(&session_id, std::time::Duration::from_secs(10))
        .map_err(|e| e.to_string())?;

    let (mut c_relay, mut c_crypto, c_keys, c_msg) =
        ctrl.begin(&session_id).map_err(|e| e.to_string())?;
    ctrl.announce(&mut c_relay, &c_msg)
        .map_err(|e| e.to_string())?;
    ctrl.finish_with_resend(
        &mut c_relay,
        &mut c_crypto,
        &c_keys,
        &session_id,
        Some(&c_msg),
    )
    .map_err(|e| e.to_string())?;
    ctrl.mark_ready(&session_id).map_err(|e| e.to_string())?;

    // ER-711d：opt-in 文件传输（同一加密会话，单未 ACK 块背压）。
    if let Ok(path) = std::env::var("ER_FILE_SEND_PATH") {
        if let Ok(data) = std::fs::read(&path) {
            let name = std::path::Path::new(&path)
                .file_name()
                .and_then(|s| s.to_str())
                .unwrap_or("file.bin");
            let id = nanos_hex().len() as u32 ^ (data.len() as u32);
            if let Ok(mut tx) = er_channels::file_stream::StreamSender::new(
                id,
                name,
                data,
                er_channels::file_transfer::DEFAULT_CHUNK,
            ) {
                let meta = c_crypto.seal(&tx.meta());
                let _ = c_relay.send(&meta);
                let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
                while !tx.done() && std::time::Instant::now() < deadline {
                    if let Some(chunk) = tx.next_chunk() {
                        let sealed = c_crypto.seal(&chunk);
                        let _ = c_relay.send(&sealed);
                    }
                    match c_relay.recv_timeout(std::time::Duration::from_millis(200)) {
                        Ok(Some(sealed)) => {
                            if let Ok(plain) = c_crypto.open(&sealed) {
                                if plain.len() >= 9
                                    && plain[0] == er_channels::file_stream::MSG_FILE_ACK
                                {
                                    let rid = u32::from_be_bytes([
                                        plain[1], plain[2], plain[3], plain[4],
                                    ]);
                                    let index = u32::from_be_bytes([
                                        plain[5], plain[6], plain[7], plain[8],
                                    ]);
                                    if rid == tx.id() {
                                        let _ = tx.ack(index);
                                    }
                                }
                            }
                        }
                        _ => {
                            if let Some(chunk) = tx.retransmit() {
                                let sealed = c_crypto.seal(&chunk);
                                let _ = c_relay.send(&sealed);
                            }
                        }
                    }
                }
            }
        }
    }
    let _ = ctrl
        .send_frames(&mut c_relay, &mut c_crypto, frames)
        .map_err(|e| e.to_string())?;

    Ok(HostResult {
        session_id,
        frames: Vec::new(),
    })
}

/// 释放主机资源。
pub fn release_host(host_id: &str) -> bool {
    registry()
        .lock()
        .map(|mut r| r.remove(host_id).is_some())
        .unwrap_or(false)
}

fn nanos_hex() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    format!("{nanos:x}")
}

/// 取某个已准备主机的 TargetAgent 与中继地址（供流式被控使用）。
///
/// 返回的是 clone：`TargetAgent` 内部只有网关客户端与身份，克隆后共享同一设备身份，
/// 不会重复绑设备。
pub fn host_target(host_id: &str) -> Result<(TargetAgent, SocketAddr), String> {
    let reg = registry()
        .lock()
        .map_err(|_| "registry poisoned".to_string())?;
    let h = reg
        .get(host_id)
        .ok_or_else(|| format!("unknown host: {host_id}"))?;
    Ok((h.target.clone(), h.relay_addr))
}

/// 读/生成被控端「临时密码」（明文存本机，服务端只存哈希）。
///
/// 与 mac_host 示例里的同名逻辑一致：8 位数字、`~/.easyremote/host_password`、权限 600。
/// 放进库里是为了让 Flutter（走 FFI）和 CLI 共用同一个密码，不会各生成一套。
pub fn ensure_host_password() -> String {
    let home = std::env::var("HOME").unwrap_or_else(|_| ".".to_string());
    let dir = format!("{home}/.easyremote");
    let path = format!("{dir}/host_password");
    if let Ok(s) = std::fs::read_to_string(&path) {
        let s = s.trim().to_string();
        if s.len() >= 6 {
            return s;
        }
    }
    // 必须使用 CSPRNG：时间戳/PID/LCG 可预测，等于固定密码。
    let pw = generate_password();
    if pw.is_empty() {
        // 随机源不可用时返回空串（上层会因长度校验失败），绝不退回可预测密码。
        return String::new();
    }
    let _ = std::fs::create_dir_all(&dir);
    let _ = std::fs::write(&path, &pw);
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600));
    }
    pw
}

/// 生成 8 位数字临时密码（CSPRNG）。
fn generate_password() -> String {
    let mut out = String::with_capacity(8);
    let mut buf = [0u8; 16];
    while out.len() < 8 {
        if getrandom::getrandom(&mut buf).is_err() {
            return String::new();
        }
        for b in buf {
            if out.len() == 8 {
                break;
            }
            out.push(char::from(b'0' + (b % 10)));
        }
    }
    out
}

/// 用已登录会话准备主机（不重复登录、不重复绑设备）。
///
/// 返回 host_id 与设备码（设备码就是这台机器在「我的设备」页里显示的那个）。
pub fn prepare_host_from_session(
    gateway: &str,
    relay: SocketAddr,
    token: &str,
    device_id: &str,
    device_code: &str,
    device_token: &str,
) -> Result<(String, String), String> {
    let target =
        TargetAgent::from_session(gateway, token, device_id, device_code, device_token, relay)
            .map_err(|e| e.to_string())?;
    let host_id = format!("host-{}", nanos_hex());
    registry()
        .lock()
        .map_err(|_| "registry poisoned".to_string())?
        .insert(
            host_id.clone(),
            PreparedHost {
                host_id: host_id.clone(),
                device_code: device_code.to_string(),
                captcha: String::new(),
                target,
                relay_addr: relay,
            },
        );
    Ok((host_id, device_code.to_string()))
}
#[cfg(test)]
mod tests {
    use super::generate_password;

    // ER-403：临时密码必须是 CSPRNG 生成的 8 位数字，且不能两次相同。
    #[test]
    fn host_password_is_random_digits() {
        let a = generate_password();
        let b = generate_password();
        assert_eq!(a.len(), 8);
        assert!(a.chars().all(|c| c.is_ascii_digit()));
        assert_ne!(a, b, "两次生成不应相同（LCG/时间/PID 会可预测）");
    }
}
