//! 剪贴板同步（PRD §5.5）。
//!
//! 难点不是搬运，而是**防回声**：A 改了剪贴板 → 发给 B → B 设置剪贴板 →
//! B 的监听器又把它当成新内容发回 A，形成无限循环。
//! 解法是「来源标记 + 内容哈希去重」：收到对端内容时记录其哈希，
//! 本地监听器看到相同哈希就不再回传。

use crate::file_transfer::sha256_hex;

/// 剪贴板内容类型。只同步白名单内的类型（避免传文件流/图片把链路打爆）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClipKind {
    Text,
    Html,
    Rtf,
}

impl ClipKind {
    pub fn is_allowed(self) -> bool {
        matches!(self, ClipKind::Text | ClipKind::Html | ClipKind::Rtf)
    }
}

/// 一次剪贴板更新。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClipUpdate {
    pub kind: ClipKind,
    pub data: String,
    /// 内容哈希，用于去重
    pub hash: String,
}

impl ClipUpdate {
    pub fn new(kind: ClipKind, data: impl Into<String>) -> Self {
        let data = data.into();
        let hash = sha256_hex(data.as_bytes());
        Self { kind, data, hash }
    }
}

/// 同步状态机。
#[derive(Debug)]
pub struct ClipboardSync {
    /// 单条内容上限（字节）。超过直接拒绝，避免大图/大段文本打爆链路。
    pub max_bytes: usize,
    /// 最近一次本机发出或对端设置的内容哈希
    last_hash: Option<String>,
    /// 抑制回声的次数（对端设置后会触发一次本地监听回调）
    suppress_once: bool,
    /// 已同步条目数
    synced: u64,
    /// 被丢弃的次数
    dropped: u64,
}

impl Default for ClipboardSync {
    fn default() -> Self {
        Self::new(1024 * 1024)
    }
}

impl ClipboardSync {
    pub fn new(max_bytes: usize) -> Self {
        Self {
            max_bytes,
            last_hash: None,
            suppress_once: false,
            synced: 0,
            dropped: 0,
        }
    }

    /// 本机剪贴板变化 → 决定是否发往对端。
    /// 返回 Some 表示要发；None 表示应丢弃（类型不允许/过大/是回声）。
    pub fn on_local_change(&mut self, update: &ClipUpdate) -> Option<ClipUpdate> {
        if !update.kind.is_allowed() {
            self.dropped += 1;
            return None;
        }
        if update.data.len() > self.max_bytes {
            self.dropped += 1;
            return None;
        }
        // 回声：这是我们刚替对端设置到本地剪贴板的内容
        if self.suppress_once && self.last_hash.as_deref() == Some(update.hash.as_str()) {
            self.suppress_once = false;
            self.dropped += 1;
            return None;
        }
        if self.last_hash.as_deref() == Some(update.hash.as_str()) {
            self.dropped += 1;
            return None;
        }
        self.last_hash = Some(update.hash.clone());
        self.synced += 1;
        Some(update.clone())
    }

    /// 对端发来内容 → 决定是否应用到本地剪贴板。
    /// 返回 Some 表示应写入本地；None 表示忽略。
    pub fn on_remote_update(&mut self, update: &ClipUpdate) -> Option<ClipUpdate> {
        if !update.kind.is_allowed() {
            self.dropped += 1;
            return None;
        }
        if update.data.len() > self.max_bytes {
            self.dropped += 1;
            return None;
        }
        if self.last_hash.as_deref() == Some(update.hash.as_str()) {
            self.dropped += 1;
            return None;
        }
        self.last_hash = Some(update.hash.clone());
        self.suppress_once = true; // 下次本地监听回调要静音
        self.synced += 1;
        Some(update.clone())
    }

    pub fn synced(&self) -> u64 {
        self.synced
    }
    pub fn dropped(&self) -> u64 {
        self.dropped
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn text_syncs_both_ways() {
        let mut a = ClipboardSync::default();
        let mut b = ClipboardSync::default();

        let u = ClipUpdate::new(ClipKind::Text, "hello");
        let out = a.on_local_change(&u).expect("should send");
        let applied = b.on_remote_update(&out).expect("should apply");
        assert_eq!(applied.data, "hello");
        assert_eq!(a.synced(), 1);
        assert_eq!(b.synced(), 1);
    }

    #[test]
    fn no_echo_loop() {
        let mut a = ClipboardSync::default();
        let mut b = ClipboardSync::default();

        let u = ClipUpdate::new(ClipKind::Text, "ping");
        let out = a.on_local_change(&u).unwrap();
        b.on_remote_update(&out).unwrap();

        // B 应用后，本地监听器会看到同一内容 —— 必须被静音
        let echo = ClipUpdate::new(ClipKind::Text, "ping");
        assert!(
            b.on_local_change(&echo).is_none(),
            "echo must be suppressed"
        );

        // 而真正的新内容仍能发出
        let fresh = ClipUpdate::new(ClipKind::Text, "pong");
        assert!(b.on_local_change(&fresh).is_some());
    }

    #[test]
    fn oversized_content_rejected() {
        let mut s = ClipboardSync::new(1024);
        let big = ClipUpdate::new(ClipKind::Text, "x".repeat(2048));
        assert!(s.on_local_change(&big).is_none());
        assert_eq!(s.dropped(), 1);
    }

    #[test]
    fn duplicate_content_not_evaluated() {
        let mut s = ClipboardSync::default();
        let u = ClipUpdate::new(ClipKind::Text, "same");
        assert!(s.on_local_change(&u).is_some());
        let again = ClipUpdate::new(ClipKind::Text, "same");
        assert!(s.on_local_change(&again).is_none());
        assert_eq!(s.dropped(), 1);
    }

    #[test]
    fn same_hash_different_kind_still_dedups() {
        // 同内容不同格式：哈希相同即视为同一条，避免重复搬运
        let mut s = ClipboardSync::default();
        let t = ClipUpdate::new(ClipKind::Text, "abc");
        let h = ClipUpdate::new(ClipKind::Html, "abc");
        assert_eq!(t.hash, h.hash);
        assert!(s.on_local_change(&t).is_some());
        assert!(s.on_local_change(&h).is_none());
    }

    #[test]
    fn kind_whitelist() {
        assert!(ClipKind::Text.is_allowed());
        assert!(ClipKind::Html.is_allowed());
        assert!(ClipKind::Rtf.is_allowed());
    }
}
/// 剪贴板后端：真实系统或测试替身。
pub trait ClipboardBackend {
    fn read_text(&mut self) -> Result<String, String>;
    fn write_text(&mut self, text: &str) -> Result<(), String>;
}

/// 系统剪贴板（macOS pbpaste/pbcopy；Linux xclip；Windows PowerShell）。
pub struct CommandClipboard;

impl ClipboardBackend for CommandClipboard {
    fn read_text(&mut self) -> Result<String, String> {
        #[cfg(target_os = "macos")]
        let out = std::process::Command::new("pbpaste").output();
        #[cfg(target_os = "linux")]
        let out = std::process::Command::new("xclip")
            .args(["-selection", "clipboard", "-o"])
            .output();
        #[cfg(windows)]
        let out = std::process::Command::new("powershell")
            .args(["-NoProfile", "-Command", "Get-Clipboard"])
            .output();
        #[cfg(not(any(target_os = "macos", target_os = "linux", windows)))]
        let out: Result<std::process::Output, std::io::Error> =
            Err(std::io::Error::other("clipboard unsupported"));
        let out = out.map_err(|e| e.to_string())?;
        if !out.status.success() {
            return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
        }
        Ok(String::from_utf8_lossy(&out.stdout)
            .trim_end_matches('\n')
            .to_string())
    }

    fn write_text(&mut self, text: &str) -> Result<(), String> {
        use std::io::Write;
        #[cfg(target_os = "macos")]
        let child = std::process::Command::new("pbcopy")
            .stdin(std::process::Stdio::piped())
            .spawn();
        #[cfg(target_os = "linux")]
        let child = std::process::Command::new("xclip")
            .args(["-selection", "clipboard", "-i"])
            .stdin(std::process::Stdio::piped())
            .spawn();
        #[cfg(windows)]
        let child = std::process::Command::new("powershell")
            .args(["-NoProfile", "-Command", "$input | Set-Clipboard"])
            .stdin(std::process::Stdio::piped())
            .spawn();
        #[cfg(not(any(target_os = "macos", target_os = "linux", windows)))]
        let mut child: Result<std::process::Child, std::io::Error> =
            Err(std::io::Error::other("clipboard unsupported"));
        let mut child = child.map_err(|e| e.to_string())?;
        if let Some(stdin) = child.stdin.as_mut() {
            stdin
                .write_all(text.as_bytes())
                .map_err(|e| e.to_string())?;
        }
        let status = child.wait().map_err(|e| e.to_string())?;
        if !status.success() {
            return Err("clipboard writer failed".to_string());
        }
        Ok(())
    }
}

/// 剪贴板服务：开关关闭时零读写；远端更新受白名单/大小/回声约束。
pub struct ClipboardService<B: ClipboardBackend> {
    sync: ClipboardSync,
    backend: B,
    enabled: bool,
}

impl<B: ClipboardBackend> ClipboardService<B> {
    pub fn new(backend: B, enabled: bool) -> Self {
        Self {
            sync: ClipboardSync::default(),
            backend,
            enabled,
        }
    }

    pub fn set_enabled(&mut self, enabled: bool) {
        self.enabled = enabled;
    }

    pub fn enabled(&self) -> bool {
        self.enabled
    }

    /// 本机轮询：只有开关开启才读取，返回需要发送的内容。
    pub fn poll_local(&mut self) -> Result<Option<ClipUpdate>, String> {
        if !self.enabled {
            return Ok(None);
        }
        let text = self.backend.read_text()?;
        if text.is_empty() {
            return Ok(None);
        }
        let update = ClipUpdate::new(ClipKind::Text, text);
        Ok(self.sync.on_local_change(&update))
    }

    /// 处理对端内容：只有开关开启才写本机；返回是否写入。
    pub fn apply_remote(&mut self, update: &ClipUpdate) -> Result<bool, String> {
        if !self.enabled {
            return Ok(false);
        }
        let Some(accepted) = self.sync.on_remote_update(update) else {
            return Ok(false);
        };
        self.backend.write_text(&accepted.data)?;
        Ok(true)
    }

    pub fn synced(&self) -> u64 {
        self.sync.synced()
    }

    pub fn dropped(&self) -> u64 {
        self.sync.dropped()
    }
}

#[cfg(test)]
mod service_tests {
    use super::*;

    #[derive(Default)]
    struct FakeClipboard {
        value: String,
        reads: u32,
        writes: u32,
    }

    impl ClipboardBackend for FakeClipboard {
        fn read_text(&mut self) -> Result<String, String> {
            self.reads += 1;
            Ok(self.value.clone())
        }
        fn write_text(&mut self, text: &str) -> Result<(), String> {
            self.writes += 1;
            self.value = text.to_string();
            Ok(())
        }
    }

    #[test]
    fn disabled_means_zero_reads_and_writes() {
        let mut svc = ClipboardService::new(FakeClipboard::default(), false);
        assert!(svc.poll_local().unwrap().is_none());
        let update = ClipUpdate::new(ClipKind::Text, "hello");
        assert!(!svc.apply_remote(&update).unwrap());
        assert_eq!(svc.backend.reads, 0);
        assert_eq!(svc.backend.writes, 0);
    }

    #[test]
    fn enabled_syncs_and_suppresses_echo() {
        let mut svc = ClipboardService::new(FakeClipboard::default(), true);
        svc.backend.value = "local".into();
        let out = svc.poll_local().unwrap().expect("local change");
        assert_eq!(out.data, "local");

        let remote = ClipUpdate::new(ClipKind::Text, "remote");
        assert!(svc.apply_remote(&remote).unwrap());
        assert_eq!(svc.backend.value, "remote");
        // 本机监听器看到刚写入的内容 → 必须抑制回声。
        assert!(svc.poll_local().unwrap().is_none());
    }

    #[test]
    fn oversized_remote_is_not_written() {
        let mut svc = ClipboardService::new(FakeClipboard::default(), true);
        let big = ClipUpdate::new(ClipKind::Text, "x".repeat(2 * 1024 * 1024));
        assert!(!svc.apply_remote(&big).unwrap());
        assert_eq!(svc.backend.writes, 0);
    }
}
/// 会话 data stream 上的剪贴板消息标签。
pub const MSG_CLIP: u8 = 0x30;
/// 单条消息硬上限（与 ClipboardSync 默认一致，防止单包打爆链路）。
pub const MAX_CLIP_BYTES: usize = 1024 * 1024;

/// 编码为会话消息：[MSG_CLIP][kind u8][utf8...]。
pub fn encode_clip(update: &ClipUpdate) -> Result<Vec<u8>, String> {
    if update.data.len() > MAX_CLIP_BYTES {
        return Err("clipboard too large".into());
    }
    let kind = match update.kind {
        ClipKind::Text => 1u8,
        ClipKind::Html => 2u8,
        ClipKind::Rtf => 3u8,
    };
    let mut out = Vec::with_capacity(update.data.len() + 2);
    out.push(MSG_CLIP);
    out.push(kind);
    out.extend_from_slice(update.data.as_bytes());
    Ok(out)
}

/// 解码会话消息；非法/超限返回 None（上层丢弃并计数）。
pub fn decode_clip(buf: &[u8]) -> Option<ClipUpdate> {
    if buf.len() < 2 || buf[0] != MSG_CLIP || buf.len() - 2 > MAX_CLIP_BYTES {
        return None;
    }
    let kind = match buf[1] {
        1 => ClipKind::Text,
        2 => ClipKind::Html,
        3 => ClipKind::Rtf,
        _ => return None,
    };
    let data = std::str::from_utf8(&buf[2..]).ok()?;
    Some(ClipUpdate::new(kind, data))
}

#[cfg(test)]
mod codec_tests {
    use super::*;

    #[test]
    fn clip_roundtrip() {
        let u = ClipUpdate::new(ClipKind::Html, "<b>hi</b>");
        let raw = encode_clip(&u).unwrap();
        assert_eq!(raw[0], MSG_CLIP);
        assert_eq!(decode_clip(&raw).unwrap(), u);
    }

    #[test]
    fn clip_rejects_bad_kind_and_truncation() {
        assert!(decode_clip(&[MSG_CLIP]).is_none());
        assert!(decode_clip(&[MSG_CLIP, 9, b'x']).is_none());
        assert!(decode_clip(&[0x99, 1, b'x']).is_none());
    }

    #[test]
    fn clip_rejects_oversize() {
        let u = ClipUpdate::new(ClipKind::Text, "x".repeat(MAX_CLIP_BYTES + 1));
        assert!(encode_clip(&u).is_err());
    }
}
