//! GPU 编码路径的**回退决策**与状态机（平台无关，可无 GPU 单测）。
//!
//! 为什么单独抽出来：零拷贝路径依赖 D3D11 设备、NVENC 会话、纹理尺寸三件事，
//! 任何一件不满足都必须**自动**退回 CPU 路径，且不能 panic、不能每帧刷日志。
//! 把「能不能走 GPU / 什么时候放弃」做成纯状态机，就可以在没有显卡的机器上把它测干净。

/// 为什么回退到 CPU 路径。日志里要能说清原因，所以带上具体数值。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GpuFallback {
    /// 没有硬件编码器（非 Windows / 无 NVIDIA / 驱动起不来 NVENC 会话）
    NoHardwareEncoder,
    /// 采集纹理尺寸与 EncodeConfig 不一致（改分辨率、跨屏拖动、DPI 变化都会触发）
    DimensionMismatch { expected: (u32, u32), actual: (u32, u32) },
    /// NVENC 注册输入纹理失败
    RegisterFailed(String),
    /// 编码阶段失败
    EncodeFailed(String),
    /// 拿不到 GPU 采集后端（DXGI 不可用、被别的进程占用）
    NotAvailable(String),
}

impl GpuFallback {
    /// 一行日志文本（产品路径与证据都用它）。
    pub fn describe(&self) -> String {
        match self {
            GpuFallback::NoHardwareEncoder => "no hardware encoder".to_string(),
            GpuFallback::DimensionMismatch { expected, actual } => format!(
                "dimension mismatch: config {}x{}, capture {}x{}",
                expected.0, expected.1, actual.0, actual.1
            ),
            GpuFallback::RegisterFailed(m) => format!("register input texture failed: {m}"),
            GpuFallback::EncodeFailed(m) => format!("gpu encode failed: {m}"),
            GpuFallback::NotAvailable(m) => format!("gpu capture unavailable: {m}"),
        }
    }

    /// 这类失败是不是「永久性」的：尺寸不匹配和硬件缺失重试也没用，
    /// 编码/注册失败可能是瞬时的（设备被独占），允许有限重试。
    pub fn is_permanent(&self) -> bool {
        matches!(self, GpuFallback::NoHardwareEncoder | GpuFallback::DimensionMismatch { .. })
    }
}

/// 状态机的三个状态。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GpuState {
    /// 还没建过 GPU 路径
    Uninitialized,
    /// 已注册（有可用的 NVENC 会话 + 输入纹理）
    Registered,
    /// 已失效，当前走 CPU 路径；reset() 后回到 Uninitialized
    Failed(GpuFallback),
}

/// GPU 路径的状态机 + 回退策略。
#[derive(Debug)]
pub struct GpuPath {
    state: GpuState,
    attempts: u32,
    max_attempts: u32,
    /// 本次退避是否已经打过日志（避免每帧刷屏）
    logged: bool,
}

impl GpuPath {
    pub fn new(max_attempts: u32) -> Self {
        Self { state: GpuState::Uninitialized, attempts: 0, max_attempts: max_attempts.max(1), logged: false }
    }

    pub fn state(&self) -> &GpuState {
        &self.state
    }

    pub fn attempts(&self) -> u32 {
        self.attempts
    }

    /// 现在应不应该尝试 GPU 路径。
    pub fn should_try(&self) -> bool {
        !matches!(self.state, GpuState::Failed(_))
    }

    /// 失效原因（供日志/诊断）。
    pub fn failure(&self) -> Option<&GpuFallback> {
        match &self.state {
            GpuState::Failed(f) => Some(f),
            _ => None,
        }
    }

    /// 首次需要打日志时返回文本（只返回一次）。
    pub fn take_log(&mut self) -> Option<String> {
        if self.logged {
            return None;
        }
        let msg = self.failure().map(|f| f.describe());
        if msg.is_some() {
            self.logged = true;
        }
        msg
    }

    /// 初始化成功：Uninitialized → Registered。
    pub fn mark_registered(&mut self) {
        self.state = GpuState::Registered;
        self.attempts = 0;
        self.logged = false;
    }

    /// 初始化/建链失败。永久性失败立刻停用；瞬时失败按 max_attempts 计数。
    pub fn mark_failed(&mut self, reason: GpuFallback) {
        self.attempts = self.attempts.saturating_add(1);
        if reason.is_permanent() || self.attempts >= self.max_attempts {
            self.state = GpuState::Failed(reason);
        }
    }

    /// 校验采集纹理尺寸。不匹配时状态迁移到 Failed 并把原因返回给调用方。
    pub fn check_dims(
        &mut self,
        expected: (u32, u32),
        actual: (u32, u32),
    ) -> Result<(), GpuFallback> {
        if expected == actual {
            return Ok(());
        }
        let reason = GpuFallback::DimensionMismatch { expected, actual };
        // 尺寸不匹配是永久性的：配置不变就不可能好，直接停用
        self.state = GpuState::Failed(reason.clone());
        Err(reason)
    }

    /// 这一轮桌面没有新帧：**不是失败**，策略不变。
    ///
    /// 保留这个显式方法，是为了让调用点（encoder.rs 的 NoFrame 分支）
    /// 把「无新帧」和「GPU 失效」在代码上一眼区分开——之前就是混在一起，
    /// 导致静止桌面时 backend 被误标成 cpu。
    pub fn on_no_frame(&mut self) {}

    /// 编码出错：可能是瞬时的，计数到上限才彻底停用。
    pub fn mark_encode_failed(&mut self, msg: impl Into<String>) {
        self.mark_failed(GpuFallback::EncodeFailed(msg.into()));
    }

    /// 失效后重建：回到 Uninitialized，允许下一帧重新尝试。
    pub fn reset(&mut self) {
        self.state = GpuState::Uninitialized;
        self.attempts = 0;
        self.logged = false;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 情形一：没有硬件编码器 → 立刻回退，且不再尝试。
    #[test]
    fn no_hardware_encoder_falls_back_without_retry() {
        let mut p = GpuPath::new(3);
        assert!(p.should_try());
        p.mark_failed(GpuFallback::NoHardwareEncoder);
        assert!(!p.should_try(), "无硬件编码器必须立刻停用");
        assert_eq!(p.state(), &GpuState::Failed(GpuFallback::NoHardwareEncoder));
        let log = p.take_log().expect("必须给出回退原因");
        assert!(log.contains("no hardware encoder"), "日志文本不对: {log}");
        assert!(p.take_log().is_none(), "同一原因只打一次日志");
    }

    /// 情形二：纹理尺寸与配置不符 → 立刻回退，并带上两边尺寸。
    #[test]
    fn dimension_mismatch_falls_back_and_reports_both_sizes() {
        let mut p = GpuPath::new(3);
        p.mark_registered();
        assert_eq!(p.state(), &GpuState::Registered);

        let err = p.check_dims((1920, 1080), (2880, 1620)).expect_err("必须报错");
        assert_eq!(err, GpuFallback::DimensionMismatch { expected: (1920, 1080), actual: (2880, 1620) });
        assert!(!p.should_try(), "尺寸不匹配必须停用 GPU 路径");
        let log = p.take_log().unwrap();
        assert!(log.contains("1920x1080") && log.contains("2880x1620"), "日志要带两边尺寸: {log}");
    }

    /// 尺寸一致时不能误判为失败。
    #[test]
    fn matching_dims_keep_gpu_path() {
        let mut p = GpuPath::new(3);
        p.mark_registered();
        assert!(p.check_dims((2880, 1620), (2880, 1620)).is_ok());
        assert!(p.should_try());
        assert!(p.failure().is_none());
    }

    /// 情形三：注册输入纹理失败 → 计数到上限后停用（瞬时失败允许重试）。
    #[test]
    fn register_failure_retries_then_falls_back() {
        let mut p = GpuPath::new(2);
        p.mark_failed(GpuFallback::RegisterFailed("nvEncRegisterResource: 4".into()));
        assert!(p.should_try(), "第一次失败还没到上限，应该允许重试");
        p.mark_failed(GpuFallback::RegisterFailed("nvEncRegisterResource: 4".into()));
        assert!(!p.should_try(), "到上限必须停用");
        let log = p.take_log().unwrap();
        assert!(log.contains("register input texture failed"), "{log}");
    }

    /// 状态机：未初始化 → 已注册 → 失效 → （reset）→ 未初始化 → 已注册。
    #[test]
    fn state_machine_transitions() {
        let mut p = GpuPath::new(3);
        assert_eq!(p.state(), &GpuState::Uninitialized);
        assert!(p.should_try());

        p.mark_registered();
        assert_eq!(p.state(), &GpuState::Registered);

        p.mark_encode_failed("nvEncEncodePicture: 8");
        assert_eq!(p.state(), &GpuState::Registered, "单次编码失败还不该停用（未到上限）");

        p.mark_encode_failed("nvEncEncodePicture: 8");
        p.mark_encode_failed("nvEncEncodePicture: 8");
        assert!(matches!(p.state(), GpuState::Failed(GpuFallback::EncodeFailed(_))));
        assert!(!p.should_try());

        p.reset();
        assert_eq!(p.state(), &GpuState::Uninitialized, "reset 后要能重建");
        assert!(p.should_try());
        p.mark_registered();
        assert_eq!(p.state(), &GpuState::Registered);
    }

    /// 任何失败都只影响「走不走 GPU」，绝不能 panic；这条用穷举失败类型来钉住。
    #[test]
    fn no_fallback_reason_panics() {
        let reasons = vec![
            GpuFallback::NoHardwareEncoder,
            GpuFallback::DimensionMismatch { expected: (1, 1), actual: (2, 2) },
            GpuFallback::RegisterFailed("x".into()),
            GpuFallback::EncodeFailed("y".into()),
            GpuFallback::NotAvailable("z".into()),
        ];
        for r in reasons {
            let mut p = GpuPath::new(1);
            p.mark_registered();
            p.mark_failed(r.clone());
            assert!(!p.should_try(), "{r:?} 应该停用");
            assert!(p.failure().is_some());
            let _ = p.take_log();
            p.reset();
            assert!(p.should_try(), "reset 后应可重试");
        }
    }
}
