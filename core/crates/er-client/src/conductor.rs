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
        .finish(&mut t_relay, &mut t_crypto, &session_id)
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

    let (mut c_relay, mut c_crypto, c_keys, c_msg) =
        ctrl.begin(&session_id).map_err(|e| e.to_string())?;
    ctrl.announce(&mut c_relay, &c_msg)
        .map_err(|e| e.to_string())?;
    ctrl.finish(&mut c_relay, &mut c_crypto, &c_keys, &session_id)
        .map_err(|e| e.to_string())?;

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
    let reg = registry().lock().map_err(|_| "registry poisoned".to_string())?;
    let h = reg.get(host_id).ok_or_else(|| format!("unknown host: {host_id}"))?;
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
    let mut seed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(12345);
    seed ^= (std::process::id() as u64).wrapping_mul(0x9E37_79B9_7F4A_7C15);
    let mut pw = String::with_capacity(8);
    for _ in 0..8 {
        seed = seed
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        pw.push(char::from(b'0' + ((seed >> 33) % 10) as u8));
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
    let target = TargetAgent::from_session(gateway, token, device_id, device_code, device_token, relay)
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
