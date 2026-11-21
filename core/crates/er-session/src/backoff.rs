//! 断线重连退避策略（PRD §5.1.3）。
//!
//! 需求：1s → 2s → 4s → 8s → 16s → 32s → 60s，之后稳定在 60s 上限。
//! 纯逻辑，可穷举验证；实际等待由调用方执行。

use std::time::Duration;

/// 退避策略。
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct BackoffPolicy {
    /// 首次重试等待（毫秒）
    pub base_ms: u64,
    /// 每次翻倍系数
    pub factor: u32,
    /// 上限（毫秒）
    pub max_ms: u64,
    /// 最多重试次数；0 表示无限
    pub max_attempts: u32,
}

impl Default for BackoffPolicy {
    fn default() -> Self {
        Self { base_ms: 1000, factor: 2, max_ms: 60_000, max_attempts: 0 }
    }
}

impl BackoffPolicy {
    /// 速通策略：测试用，避免真等 60 秒。
    pub fn fast() -> Self {
        Self { base_ms: 10, factor: 2, max_ms: 200, max_attempts: 8 }
    }

    /// 第 attempt 次重试（从 0 开始）应等待多久。
    pub fn delay(&self, attempt: u32) -> Duration {
        if self.base_ms == 0 {
            return Duration::ZERO;
        }
        let mut ms = self.base_ms;
        for _ in 0..attempt {
            ms = ms.saturating_mul(self.factor.max(1) as u64);
            if ms >= self.max_ms {
                break;
            }
        }
        Duration::from_millis(ms.min(self.max_ms))
    }

    /// 是否还应继续重试。
    pub fn should_retry(&self, attempt: u32) -> bool {
        self.max_attempts == 0 || attempt < self.max_attempts
    }

    /// 生成完整等待序列（用于日志与验收展示）。
    pub fn schedule(&self, count: u32) -> Vec<Duration> {
        (0..count).map(|i| self.delay(i)).collect()
    }
}

/// 按退避策略反复尝试，直到成功或超过总时长。
///
/// 返回 (结果, 实际重试次数)。用于「中继重启后重新注册并恢复收发」这类场景。
pub fn retry_until<T, E, F>(
    policy: BackoffPolicy,
    max_total: Duration,
    mut op: F,
) -> Result<(T, u32), E>
where
    F: FnMut() -> Result<T, E>,
{
    let start = std::time::Instant::now();
    let mut attempt = 0u32;
    loop {
        match op() {
            Ok(v) => return Ok((v, attempt)),
            Err(e) => {
                let done = !policy.should_retry(attempt) || start.elapsed() >= max_total;
                if done {
                    return Err(e);
                }
                std::thread::sleep(policy.delay(attempt));
                attempt += 1;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_schedule_matches_prd() {
        let p = BackoffPolicy::default();
        let secs: Vec<u64> = p.schedule(8).iter().map(|d| d.as_secs()).collect();
        assert_eq!(secs, vec![1, 2, 4, 8, 16, 32, 60, 60]);
    }

    #[test]
    fn delay_is_capped() {
        let p = BackoffPolicy::default();
        for attempt in 0..50 {
            assert!(p.delay(attempt) <= Duration::from_secs(60), "attempt {attempt}");
        }
    }

    #[test]
    fn fast_policy_is_quick_but_capped() {
        let p = BackoffPolicy::fast();
        assert_eq!(p.delay(0), Duration::from_millis(10));
        assert_eq!(p.delay(1), Duration::from_millis(20));
        assert_eq!(p.delay(2), Duration::from_millis(40));
        for a in 0..20 {
            assert!(p.delay(a) <= Duration::from_millis(200));
        }
    }

    #[test]
    fn attempt_limit_respected() {
        let p = BackoffPolicy::fast();
        assert!(p.should_retry(0));
        assert!(p.should_retry(7));
        assert!(!p.should_retry(8));

        let unlimited = BackoffPolicy { max_attempts: 0, ..BackoffPolicy::fast() };
        assert!(unlimited.should_retry(1000));
    }

    #[test]
    fn zero_base_means_no_wait() {
        let p = BackoffPolicy { base_ms: 0, ..BackoffPolicy::default() };
        assert_eq!(p.delay(5), Duration::ZERO);
    }

    #[test]
    fn schedule_is_monotonic_non_decreasing() {
        let p = BackoffPolicy::default();
        let s = p.schedule(10);
        for w in s.windows(2) {
            assert!(w[1] >= w[0]);
        }
    }
}
