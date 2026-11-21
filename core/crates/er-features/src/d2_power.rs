//! D2 防睡眠：会话期间阻止系统空闲睡眠。
//!
//! 平台实现（Windows `SetThreadExecutionState` / macOS `IOPMAssertion`）
//! 放在各自 crate 里；本模块只定义契约、RAII guard 与可测试的假实现。
//!
//! 关键约束（PRD §5.6）：异常路径也必须释放，因此用 `Drop` 保证。

/// 电源抑制错误。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PowerError {
    /// 平台拒绝（权限/策略）
    Denied,
    /// 平台不支持
    Unsupported,
}

impl std::fmt::Display for PowerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            PowerError::Denied => write!(f, "power assertion denied"),
            PowerError::Unsupported => write!(f, "power assertion unsupported"),
        }
    }
}

impl std::error::Error for PowerError {}

/// 电源抑制器。同一实例可重复 acquire/release（幂等）。
pub trait SleepInhibitor {
    /// 申请阻止系统睡眠；是否需要同时阻止息屏由 `keep_display` 决定。
    fn acquire(&mut self, keep_display: bool) -> Result<(), PowerError>;
    /// 释放抑制；未持有时调用应成功（幂等）。
    fn release(&mut self) -> Result<(), PowerError>;
    /// 当前是否持有抑制。
    fn is_held(&self) -> bool;
}

/// RAII guard：离开作用域自动释放。
///
/// 会话结束、panic、提前 return 都会走 Drop，避免"电脑永远不睡"的残留。
pub struct SleepGuard<'a, T: SleepInhibitor> {
    inhibitor: &'a mut T,
    active: bool,
}

impl<'a, T: SleepInhibitor> SleepGuard<'a, T> {
    /// 申请抑制并返回 guard。
    pub fn new(inhibitor: &'a mut T, keep_display: bool) -> Result<Self, PowerError> {
        inhibitor.acquire(keep_display)?;
        Ok(Self {
            inhibitor,
            active: true,
        })
    }

    /// 手动提前释放。
    pub fn release_now(mut self) -> Result<(), PowerError> {
        self.active = false;
        self.inhibitor.release()
    }
}

impl<T: SleepInhibitor> Drop for SleepGuard<'_, T> {
    fn drop(&mut self) {
        if self.active {
            let _ = self.inhibitor.release();
        }
    }
}

/// 假实现：记录调用次数，用于验证生命周期语义。
#[derive(Debug, Default)]
pub struct FakeInhibitor {
    held: bool,
    acquires: u32,
    releases: u32,
    last_keep_display: bool,
}

impl FakeInhibitor {
    pub fn new() -> Self {
        Self::default()
    }
    pub fn acquire_count(&self) -> u32 {
        self.acquires
    }
    pub fn release_count(&self) -> u32 {
        self.releases
    }
    pub fn last_keep_display(&self) -> bool {
        self.last_keep_display
    }
}

impl SleepInhibitor for FakeInhibitor {
    fn acquire(&mut self, keep_display: bool) -> Result<(), PowerError> {
        self.acquires += 1;
        self.last_keep_display = keep_display;
        self.held = true;
        Ok(())
    }

    fn release(&mut self) -> Result<(), PowerError> {
        self.releases += 1;
        self.held = false;
        Ok(())
    }

    fn is_held(&self) -> bool {
        self.held
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn guard_acquires_and_releases_on_drop() {
        let mut fake = FakeInhibitor::new();
        {
            let g = SleepGuard::new(&mut fake, true).unwrap();
            assert!(g.active);
        }
        assert!(!fake.is_held(), "drop must release");
        assert_eq!(fake.acquire_count(), 1);
        assert_eq!(fake.release_count(), 1);
        assert!(fake.last_keep_display());
    }

    #[test]
    fn guard_releases_on_early_return() {
        fn run(fake: &mut FakeInhibitor) {
            let _g = SleepGuard::new(fake, false).unwrap();
            // 模拟提前 return
            if true {
                return;
            }
        }
        let mut fake = FakeInhibitor::new();
        run(&mut fake);
        assert!(!fake.is_held());
        assert_eq!(fake.release_count(), 1);
    }

    #[test]
    fn manual_release_is_not_double_released() {
        let mut fake = FakeInhibitor::new();
        let g = SleepGuard::new(&mut fake, true).unwrap();
        g.release_now().unwrap();
        // 手动释放后 drop 不应再释放一次
        assert_eq!(fake.release_count(), 1);
    }

    #[test]
    fn display_follows_privacy_screen_setting() {
        // 隐私屏开启时不需要阻止息屏（屏幕本来就黑）
        let mut fake = FakeInhibitor::new();
        {
            let _g = SleepGuard::new(&mut fake, false).unwrap();
        }
        assert!(!fake.last_keep_display());
    }

    #[test]
    fn release_without_acquire_is_ok() {
        let mut fake = FakeInhibitor::new();
        assert!(fake.release().is_ok(), "release must be idempotent");
    }
}
