//! ABR 自适应码率（PRD §5.2.4）。
//!
//! 输入是链路反馈（丢包率、RTT、发送队列积压），输出是下一档编码参数。
//! 纯逻辑、无 IO，因此可以穷举场景单测。
//!
//! 控制策略（保守优先，避免画面抖动）：
//!   - 丢包 > 8% 或队列积压 > 200ms：激进降档（一次降两级）
//!   - 丢包 2%–8% 或 RTT > 250ms 或队列 > 80ms：温和降档（一级）
//!   - 链路健康连续 3 个周期：升一级
//!   - 任何调整都受上下限约束

/// 一档画质参数。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct QualityLevel {
    pub name: &'static str,
    pub bitrate_kbps: u32,
    pub fps: u32,
    /// 降采样比例分母：1 表示原始分辨率，2 表示半分辨率
    pub downscale: u32,
}

/// 预设档位，从高到低。
pub const LADDER: [QualityLevel; 6] = [
    QualityLevel {
        name: "1080p60",
        bitrate_kbps: 8000,
        fps: 60,
        downscale: 1,
    },
    QualityLevel {
        name: "1080p30",
        bitrate_kbps: 5000,
        fps: 30,
        downscale: 1,
    },
    QualityLevel {
        name: "900p30",
        bitrate_kbps: 3500,
        fps: 30,
        downscale: 1,
    },
    QualityLevel {
        name: "720p30",
        bitrate_kbps: 2500,
        fps: 30,
        downscale: 2,
    },
    QualityLevel {
        name: "540p30",
        bitrate_kbps: 1500,
        fps: 30,
        downscale: 2,
    },
    QualityLevel {
        name: "360p15",
        bitrate_kbps: 800,
        fps: 15,
        downscale: 3,
    },
];

/// 链路反馈。
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct LinkFeedback {
    pub loss_pct: f32,
    pub rtt_ms: u32,
    pub queue_ms: u32,
}

/// 一次决策。
#[derive(Debug, Clone, PartialEq)]
pub struct Decision {
    pub level: QualityLevel,
    pub changed: bool,
    pub reason: &'static str,
}

/// ABR 控制器。
#[derive(Debug)]
pub struct AbrController {
    index: usize,
    healthy_streak: u32,
    /// 连续"严重拥塞"周期数：需要连续 2 次才降档，
    /// 避免周期边界上偶尔丢一帧（假丢包）就掉两档。
    severe_streak: u32,
    /// 连续"轻微拥塞"周期数
    mild_streak: u32,
    since_change: u32,
    min_interval_cycles: u32,
}

impl Default for AbrController {
    fn default() -> Self {
        Self::new(0)
    }
}

impl AbrController {
    pub fn new(start_index: usize) -> Self {
        Self {
            index: start_index.min(LADDER.len() - 1),
            healthy_streak: 0,
            severe_streak: 0,
            mild_streak: 0,
            // 初始即允许立即降档：拥塞发生时不能等一个周期，
            // 否则首帧反馈就被忽略（实测会漏掉突发丢包）。
            since_change: 2,
            min_interval_cycles: 1,
        }
    }

    pub fn current(&self) -> QualityLevel {
        LADDER[self.index]
    }

    pub fn healthy_streak(&self) -> u32 {
        self.healthy_streak
    }

    pub fn severe_streak(&self) -> u32 {
        self.severe_streak
    }

    pub fn mild_streak(&self) -> u32 {
        self.mild_streak
    }

    pub fn index(&self) -> usize {
        self.index
    }

    pub fn on_feedback(&mut self, fb: &LinkFeedback) -> Decision {
        self.since_change += 1;

        let severe = fb.loss_pct > 8.0 || fb.queue_ms > 200;
        let mild = fb.loss_pct > 2.0 || fb.queue_ms > 80 || fb.rtt_ms > 250;
        let healthy = fb.loss_pct < 1.0 && fb.queue_ms < 40 && fb.rtt_ms < 120;

        // 迟滞：单周期的假丢包（周期边界/乱序）不应触发降档
        self.severe_streak = if severe { self.severe_streak + 1 } else { 0 };
        self.mild_streak = if mild && !severe {
            self.mild_streak + 1
        } else {
            0
        };
        let severe = severe && self.severe_streak >= 2;
        let mild = mild && self.mild_streak >= 2;

        if severe {
            self.healthy_streak = 0;
            if self.index + 2 < LADDER.len() && self.since_change > self.min_interval_cycles {
                self.index += 2;
                self.since_change = 0;
                return self.decided("丢包/积压严重，连降两档");
            }
            if self.index + 1 < LADDER.len() && self.since_change > self.min_interval_cycles {
                self.index += 1;
                self.since_change = 0;
                return self.decided("丢包/积压严重，降一档");
            }
            return self.unchanged("已到最低档");
        }

        if mild {
            self.healthy_streak = 0;
            if self.index + 1 < LADDER.len() && self.since_change > self.min_interval_cycles {
                self.index += 1;
                self.since_change = 0;
                return self.decided("轻微拥塞，降一档");
            }
            return self.unchanged("轻微拥塞，保持");
        }

        if healthy {
            // 用"漏桶"而不是"清零"：单周期的噪声（周期边界丢一帧）不该把
            // 已经积累的健康周期全抹掉，否则 ABR 会变成单向棘轮——
            // 实测 15% 丢包降档后把丢包切回 0，40 个周期都没能升档。
            self.healthy_streak += 1;
            if self.healthy_streak >= 3
                && self.index > 0
                && self.since_change > self.min_interval_cycles
            {
                self.index -= 1;
                self.healthy_streak = 0;
                self.since_change = 0;
                return self.decided("链路健康，升一档");
            }
            return self.unchanged("链路健康，保持");
        }

        // 中性/轻微异常：只削一格，不清零
        self.healthy_streak = self.healthy_streak.saturating_sub(1);
        self.unchanged("反馈中性，保持")
    }

    fn decided(&self, reason: &'static str) -> Decision {
        Decision {
            level: LADDER[self.index],
            changed: true,
            reason,
        }
    }

    fn unchanged(&self, reason: &'static str) -> Decision {
        Decision {
            level: LADDER[self.index],
            changed: false,
            reason,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fb(loss: f32, rtt: u32, queue: u32) -> LinkFeedback {
        LinkFeedback {
            loss_pct: loss,
            rtt_ms: rtt,
            queue_ms: queue,
        }
    }

    #[test]
    fn starts_at_requested_level() {
        assert_eq!(AbrController::new(0).current().name, "1080p60");
        assert_eq!(AbrController::new(3).current().name, "720p30");
        assert_eq!(AbrController::new(99).current().name, "360p15");
    }

    #[test]
    fn severe_loss_drops_two_levels() {
        let mut c = AbrController::new(0);
        // 迟滞后需连续两周期严重拥塞
        c.on_feedback(&fb(15.0, 80, 10));
        let d = c.on_feedback(&fb(15.0, 80, 10));
        assert!(d.changed);
        assert_eq!(c.index(), 2);
    }

    #[test]
    fn mild_loss_drops_one_level() {
        let mut c = AbrController::new(0);
        c.on_feedback(&fb(4.0, 80, 10));
        let d = c.on_feedback(&fb(4.0, 80, 10));
        assert!(d.changed);
        assert_eq!(c.index(), 1);
    }

    #[test]
    fn high_rtt_counts_as_congestion() {
        let mut c = AbrController::new(0);
        c.on_feedback(&fb(0.0, 400, 10));
        c.on_feedback(&fb(0.0, 400, 10));
        assert_eq!(c.index(), 1);
    }

    #[test]
    fn queue_backlog_is_severe() {
        let mut c = AbrController::new(0);
        c.on_feedback(&fb(0.5, 60, 250));
        c.on_feedback(&fb(0.5, 60, 250));
        assert_eq!(c.index(), 2);
    }

    #[test]
    fn healthy_streak_required_to_upgrade() {
        let mut c = AbrController::new(3);
        c.on_feedback(&fb(0.0, 50, 10));
        assert_eq!(c.index(), 3);
        c.on_feedback(&fb(0.0, 50, 10));
        assert_eq!(c.index(), 3);
        let d = c.on_feedback(&fb(0.0, 50, 10));
        assert!(d.changed);
        assert_eq!(c.index(), 2);
    }

    #[test]
    fn cannot_exceed_top_or_bottom() {
        let mut c = AbrController::new(0);
        for _ in 0..5 {
            c.on_feedback(&fb(0.0, 40, 5));
        }
        assert_eq!(c.index(), 0);

        let mut c = AbrController::new(LADDER.len() - 1);
        for _ in 0..5 {
            c.on_feedback(&fb(50.0, 900, 500));
        }
        assert_eq!(c.index(), LADDER.len() - 1);
    }

    #[test]
    fn neutral_feedback_makes_no_change() {
        let mut c = AbrController::new(2);
        let d = c.on_feedback(&fb(1.5, 150, 50));
        assert!(!d.changed);
        assert_eq!(c.index(), 2);
    }

    #[test]
    fn ladder_is_monotonic() {
        for w in LADDER.windows(2) {
            assert!(w[0].bitrate_kbps > w[1].bitrate_kbps);
            assert!(w[0].fps >= w[1].fps);
            assert!(w[0].downscale <= w[1].downscale);
        }
    }

    /// 单周期的假丢包（周期边界/乱序）不得触发降档：
    /// 这是真实会话里 "0% 丢包也掉到最低档" 的直接原因。
    #[test]
    fn single_cycle_spike_does_not_downgrade() {
        let mut c = AbrController::new(0);
        // 一个周期报 50% 丢包，紧接着恢复正常
        c.on_feedback(&fb(50.0, 40, 10));
        assert_eq!(c.index(), 0, "单周期尖峰不应降档");
        let d = c.on_feedback(&fb(0.0, 40, 10));
        assert!(!d.changed);
        assert_eq!(c.index(), 0);
    }

    /// 连续两周期严重拥塞才降档。
    #[test]
    fn sustained_congestion_downgrades() {
        let mut c = AbrController::new(0);
        c.on_feedback(&fb(20.0, 40, 10));
        assert_eq!(c.index(), 0);
        let d = c.on_feedback(&fb(20.0, 40, 10));
        assert!(d.changed);
        assert_eq!(c.index(), 2);
    }

    /// 棘轮回归：健康周期之间夹着单个噪声周期，仍然必须能升档。
    #[test]
    fn noisy_healthy_streak_still_upgrades() {
        let mut c = AbrController::new(3);
        for i in 0..24 {
            if i % 4 != 3 {
                c.on_feedback(&fb(0.0, 30, 10));
            } else {
                // 每 4 个周期才有 1 个噪声周期（真实场景比例），
                // 50% 噪声本身就该判为链路不好、不该升档。
                c.on_feedback(&fb(3.0, 30, 10));
            }
        }
        assert!(c.index() < 3, "噪声不应阻止升档，实际 index={}", c.index());
    }

    /// 真实会话复现：先连续严重拥塞降到最低档，再持续健康 40 个周期，
    /// 必须能升回来。这条钉住"单向棘轮"。
    #[test]
    fn downgrade_then_recover() {
        let mut c = AbrController::new(0);
        // 降到最低档
        for _ in 0..12 {
            c.on_feedback(&fb(20.0, 30, 10));
        }
        let bottom = c.index();
        assert!(bottom > 0, "应当已经降档");
        // 链路恢复：40 个健康周期
        let mut ups = 0;
        for _ in 0..40 {
            let d = c.on_feedback(&fb(0.0, 10, 5));
            if d.changed {
                ups += 1;
            }
        }
        assert!(
            ups > 0,
            "健康 40 周期后必须升档，实际 ups=0，bottom={}",
            bottom
        );
        assert!(c.index() < bottom);
    }
}
