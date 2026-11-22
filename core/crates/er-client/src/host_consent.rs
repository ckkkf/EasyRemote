use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Condvar, Mutex};
use std::time::{Duration, Instant};

/// 本地授权请求快照（UI 轮询）。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ConsentSnapshot {
    pub session_id: String,
    pub controller_label: String,
    pub seconds_left: u64,
}

struct Pending {
    session_id: String,
    label: String,
    deadline: Instant,
    decision: Option<bool>,
}

/// 每个被控流一份的本地授权闸门。
///
/// 硬约束：没有任何路径可以自动同意。只有 decide()（由本机 UI 经 FFI 调用）
/// 才能让 wait() 返回 true；超时或 stop 一律按拒绝处理。
pub struct ConsentBroker {
    inner: Mutex<Option<Pending>>,
    cv: Condvar,
}

impl Default for ConsentBroker {
    fn default() -> Self {
        Self {
            inner: Mutex::new(None),
            cv: Condvar::new(),
        }
    }
}

impl ConsentBroker {
    pub fn new() -> Self {
        Self::default()
    }

    /// 登记一次授权请求。
    pub fn request(&self, session_id: &str, label: &str, timeout: Duration) {
        let mut guard = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        *guard = Some(Pending {
            session_id: session_id.to_string(),
            label: label.to_string(),
            deadline: Instant::now() + timeout,
            decision: None,
        });
        self.cv.notify_all();
    }

    /// 当前 pending 快照；已过期返回 None。
    pub fn pending(&self) -> Option<ConsentSnapshot> {
        let guard = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        let p = guard.as_ref()?;
        if Instant::now() >= p.deadline {
            return None;
        }
        Some(ConsentSnapshot {
            session_id: p.session_id.clone(),
            controller_label: p.label.clone(),
            seconds_left: p
                .deadline
                .saturating_duration_since(Instant::now())
                .as_secs(),
        })
    }

    /// 本机决策。session 不匹配（远程/旧会话）返回 false 且不改状态。
    pub fn decide(&self, session_id: &str, accepted: bool) -> bool {
        let mut guard = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        let Some(p) = guard.as_mut() else {
            return false;
        };
        if p.session_id != session_id || Instant::now() >= p.deadline {
            return false;
        }
        p.decision = Some(accepted);
        self.cv.notify_all();
        true
    }

    /// 阻塞等待决策。true=本机接受；false=超时/无决策；stop 时 Err。
    pub fn wait(&self, stop: &AtomicBool, timeout: Duration) -> Result<bool, String> {
        let deadline = Instant::now() + timeout;
        let mut guard = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        loop {
            if stop.load(Ordering::Relaxed) {
                *guard = None;
                return Err("stopped".into());
            }
            let Some(p) = guard.as_mut() else {
                return Ok(false);
            };
            if let Some(accepted) = p.decision.take() {
                *guard = None;
                return Ok(accepted);
            }
            let now = Instant::now();
            if now >= p.deadline || now >= deadline {
                *guard = None;
                return Ok(false);
            }
            let wait_for = p.deadline.min(deadline).saturating_duration_since(now);
            let (next, _) = self
                .cv
                .wait_timeout(guard, wait_for.min(Duration::from_millis(100)))
                .unwrap_or_else(|e| e.into_inner());
            guard = next;
        }
    }

    pub fn clear(&self) {
        *self.inner.lock().unwrap_or_else(|e| e.into_inner()) = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::AtomicBool;

    // ER-102 核心红线：没有本地决策时，等待方绝不能得到“接受”。
    // 测试名与任务卡验收断言逐字对应，保留 PascalCase。
    #[allow(non_snake_case)]
    #[test]
    fn TestHostNeverAutoConsents() {
        let b = ConsentBroker::new();
        let stop = AtomicBool::new(false);
        b.request("ses-1", "远程设备", Duration::from_millis(80));
        let accepted = b
            .wait(&stop, Duration::from_millis(80))
            .expect("not stopped");
        assert!(!accepted, "没有本地决策时必须拒绝/超时");
        assert!(b.pending().is_none(), "超时后必须清掉 pending");
    }

    #[test]
    fn local_accept_only_for_matching_session() {
        let b = ConsentBroker::new();
        let stop = AtomicBool::new(false);
        b.request("ses-2", "远程设备", Duration::from_secs(5));
        assert!(!b.decide("other", true), "其它 session 不能决策");
        assert!(b.decide("ses-2", true));
        assert!(b.wait(&stop, Duration::from_millis(200)).unwrap());
    }

    #[test]
    fn local_reject_is_propagated() {
        let b = ConsentBroker::new();
        let stop = AtomicBool::new(false);
        b.request("ses-3", "远程设备", Duration::from_secs(5));
        assert!(b.decide("ses-3", false));
        assert!(!b.wait(&stop, Duration::from_millis(200)).unwrap());
    }
}
