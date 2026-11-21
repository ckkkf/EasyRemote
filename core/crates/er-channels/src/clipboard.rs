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
