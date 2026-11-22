//! 链路反馈与 ABR 数据面胶水（PRD §5.2.4）。
//!
//! 数据面闭环：
//!   接收侧（观众/主控）逐帧记账 → 每个周期算一次丢帧率/RTT → 通过反向通道回传报告
//!   → 发送侧（被控）把报告喂给 `AbrController::on_feedback` → 用返回的 `QualityLevel`
//!     重配 `EncodeConfig`（码率/分辨率/帧率）。
//!
//! 这里只做「记账 + 编解码 + 档位→编码参数映射」，不碰 socket，便于单测。

use er_abr::{AbrController, Decision, LinkFeedback, QualityLevel, LADDER};
use er_codec::EncodeConfig;

/// 分片级统计（来自 `er-session` 的中继重组器）。
pub use er_session::wire::ReassemblyStats;

/// 视频消息头长度：seq u32 + ts_ms u32 + width u16 + height u16 + 保留 u16。
pub const VIDEO_HEADER_LEN: usize = 14;

/// 视频消息头。
///
/// `seq` 是发送侧的**单调帧序号**：接收侧靠它算丢帧率（丢包代理/UDP 丢包都会打出空洞）。
/// `ts_ms` 是发送侧发帧时刻（同一台机器的单调钟），接收侧原样回显，发送侧据此算 RTT——
/// 两端时钟不需要对齐。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct VideoHeader {
    pub seq: u32,
    pub ts_ms: u32,
    pub width: u16,
    pub height: u16,
}

impl VideoHeader {
    pub fn encode(&self) -> [u8; VIDEO_HEADER_LEN] {
        let mut out = [0u8; VIDEO_HEADER_LEN];
        out[0..4].copy_from_slice(&self.seq.to_le_bytes());
        out[4..8].copy_from_slice(&self.ts_ms.to_le_bytes());
        out[8..10].copy_from_slice(&self.width.to_le_bytes());
        out[10..12].copy_from_slice(&self.height.to_le_bytes());
        out
    }

    pub fn decode(buf: &[u8]) -> Option<Self> {
        if buf.len() < VIDEO_HEADER_LEN {
            return None;
        }
        Some(Self {
            seq: u32::from_le_bytes([buf[0], buf[1], buf[2], buf[3]]),
            ts_ms: u32::from_le_bytes([buf[4], buf[5], buf[6], buf[7]]),
            width: u16::from_le_bytes([buf[8], buf[9]]),
            height: u16::from_le_bytes([buf[10], buf[11]]),
        })
    }
}

/// 反向通道上的链路反馈报告（固定 36 字节，单分片可承载）。
///
/// 字段是**累积值 + 周期值**双份：单条报告在反向通道上丢了也不会让发送侧失步，
/// 下一条累积值会把它盖过去（幂等 = 可靠）。
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct FeedbackReport {
    /// 报告序号（接收侧单调递增）
    pub seq: u32,
    /// 本周期应当收到的帧数（按 seq 跨度算）
    pub cycle_expected: u32,
    /// 本周期实际收到的帧数
    pub cycle_received: u32,
    /// 累积应到帧数
    pub cum_expected: u32,
    /// 累积实收帧数
    pub cum_received: u32,
    /// 本周期丢帧率（%）
    pub loss_pct: f32,
    /// 回显：最新收到那帧的发送时刻，发送侧用 now - echo 算 RTT
    pub echo_ts_ms: u32,
    pub rtt_hint_ms: u32,
    pub queue_ms: u32,
    /// 分片级：累计收到 / 累计缺失（丢包的真实度量）
    pub frag_received: u32,
    pub frag_missing: u32,
    /// 分片级丢包率（%）
    pub datagram_loss_pct: f32,
}

impl FeedbackReport {
    pub const LEN: usize = 48;

    pub fn encode(&self) -> [u8; Self::LEN] {
        let mut out = [0u8; Self::LEN];
        out[0..4].copy_from_slice(&self.seq.to_le_bytes());
        out[4..8].copy_from_slice(&self.cycle_expected.to_le_bytes());
        out[8..12].copy_from_slice(&self.cycle_received.to_le_bytes());
        out[12..16].copy_from_slice(&self.cum_expected.to_le_bytes());
        out[16..20].copy_from_slice(&self.cum_received.to_le_bytes());
        out[20..24].copy_from_slice(&self.loss_pct.to_le_bytes());
        out[24..28].copy_from_slice(&self.echo_ts_ms.to_le_bytes());
        out[28..32].copy_from_slice(&self.rtt_hint_ms.to_le_bytes());
        out[32..36].copy_from_slice(&self.queue_ms.to_le_bytes());
        out[36..40].copy_from_slice(&self.frag_received.to_le_bytes());
        out[40..44].copy_from_slice(&self.frag_missing.to_le_bytes());
        out[44..48].copy_from_slice(&self.datagram_loss_pct.to_le_bytes());
        out
    }

    pub fn decode(buf: &[u8]) -> Option<Self> {
        if buf.len() < Self::LEN {
            return None;
        }
        let f = |o: usize| u32::from_le_bytes([buf[o], buf[o + 1], buf[o + 2], buf[o + 3]]);
        Some(Self {
            seq: f(0),
            cycle_expected: f(4),
            cycle_received: f(8),
            cum_expected: f(12),
            cum_received: f(16),
            loss_pct: f32::from_le_bytes([buf[20], buf[21], buf[22], buf[23]]),
            echo_ts_ms: f(24),
            rtt_hint_ms: f(28),
            queue_ms: f(32),
            frag_received: f(36),
            frag_missing: f(40),
            datagram_loss_pct: f32::from_le_bytes([buf[44], buf[45], buf[46], buf[47]]),
        })
    }

    /// 换成 ABR 的输入。`rtt_ms` 由发送侧用 echo 算出后填。
    ///
    /// 丢包取**分片级**（有分片统计时），因为按帧算会被多分片放大：
    /// 2880x1620 关键帧约 12 个分片，链路丢 5% 数据报 → 帧丢失率 40%+。
    /// 拿帧丢失率喂 ABR 会把 5% 的链路当重度拥塞，一路踩到底。
    pub fn to_link_feedback(&self, rtt_ms: u32) -> LinkFeedback {
        let frag_total = self.frag_received as u64 + self.frag_missing as u64;
        let loss_pct = if frag_total > 0 {
            self.datagram_loss_pct
        } else {
            self.loss_pct
        };
        LinkFeedback {
            loss_pct,
            rtt_ms,
            queue_ms: self.queue_ms,
            // 反馈报文暂未携带吞吐/编码队列；用发送队列作为缓冲代理，
            // 其余保持 0（未知），ABR 不会据此误判。
            throughput_kbps: 0,
            buffer_ms: self.queue_ms,
            encode_queue: 0,
        }
    }
}

/// 接收侧记账器：每来一帧调一次 `on_frame`，每个周期调一次 `take_cycle`。
#[derive(Debug, Default)]
pub struct LinkStats {
    /// 上一周期结束时的分片累计计数，用于算「本周期」丢包率。
    /// 累计值会让丢包率永不衰减：链路恢复后仍显示历史平均，
    /// ABR 于是变成单向棘轮（实测 15%→0% 后 40 个周期都不升档）。
    last_frag_received: u64,
    last_frag_missing: u64,
    /// 观测到的最小「到达时刻 - 发送时间戳」= 链路基础时延。
    /// 用它做基线，queue_ms 才是"相对基线多排了多久"，而不是帧间隔。
    min_transit_ms: Option<u32>,
    cycle_base_seq: Option<u32>,
    cycle_max_seq: u32,
    cycle_received: u32,
    cum_base_seq: Option<u32>,
    cum_max_seq: u32,
    cum_received: u32,
    newest_ts_ms: u32,
    last_arrival_ms: u32,
    /// 相邻两帧到达间隔的最大值（用于估计接收队列积压）
    max_gap_ms: u32,
    report_seq: u32,
}

impl LinkStats {
    pub fn new() -> Self {
        Self::default()
    }

    /// 记一帧。`arrival_ms` 是接收侧本地单调毫秒钟。
    pub fn on_frame(&mut self, hdr: &VideoHeader, arrival_ms: u32) {
        // 记录基础时延（同一时钟源：发送侧 ts 与到达时刻都是毫秒单调计数）
        let transit = arrival_ms.saturating_sub(hdr.ts_ms);
        self.min_transit_ms = Some(match self.min_transit_ms {
            Some(m) if m <= transit => m,
            _ => transit,
        });
        if self.cycle_base_seq.is_none() {
            self.cycle_base_seq = Some(hdr.seq);
            self.cycle_max_seq = hdr.seq;
        }
        if hdr.seq > self.cycle_max_seq {
            self.cycle_max_seq = hdr.seq;
        }
        self.cycle_received += 1;

        if self.cum_base_seq.is_none() {
            self.cum_base_seq = Some(hdr.seq);
            self.cum_max_seq = hdr.seq;
        }
        if hdr.seq > self.cum_max_seq {
            self.cum_max_seq = hdr.seq;
        }
        self.cum_received += 1;

        if hdr.ts_ms > self.newest_ts_ms {
            self.newest_ts_ms = hdr.ts_ms;
        }
        if self.last_arrival_ms != 0 {
            let gap = arrival_ms.saturating_sub(self.last_arrival_ms);
            if gap > self.max_gap_ms {
                self.max_gap_ms = gap;
            }
        }
        self.last_arrival_ms = arrival_ms;
    }

    /// 收一个周期：返回本周期报告（累积值一并带上），并把周期计数清零。
    ///
    /// `frag` 是接收端的分片级统计（来自 `RelayClient::link_stats`），
    /// 是本报告里**丢包**的主口径。
    ///
    /// 周期内**一帧都没收到**时返回 None（没数据就没有可判定的丢帧率）。
    pub fn take_cycle(
        &mut self,
        _arrival_ms: u32,
        frag: ReassemblyStats,
    ) -> Option<FeedbackReport> {
        let (Some(cbase), cum_base) = (self.cycle_base_seq, self.cum_base_seq.unwrap_or(0)) else {
            return None;
        };
        if self.cycle_received == 0 {
            return None;
        }
        let cycle_expected = self.cycle_max_seq.saturating_sub(cbase) + 1;
        let cum_expected = self.cum_max_seq.saturating_sub(cum_base) + 1;
        let lost = cycle_expected.saturating_sub(self.cycle_received);
        let loss_pct = if cycle_expected == 0 {
            0.0
        } else {
            (lost as f32) * 100.0 / (cycle_expected as f32)
        };
        self.report_seq += 1;
        let report = FeedbackReport {
            seq: self.report_seq,
            cycle_expected,
            cycle_received: self.cycle_received,
            cum_expected,
            cum_received: self.cum_received,
            loss_pct,
            echo_ts_ms: self.newest_ts_ms,
            rtt_hint_ms: 0,
            // queue_ms 必须是「排队增量」，不是帧间隔：
            // 低帧率下帧间隔天然很大（1s 周期 4 帧 → 250ms），
            // 若把间隔当排队，ABR 会在 0% 丢包时也判成严重拥塞，
            // 降档 → 帧率更低 → 间隔更大 → 死循环降到最低档。
            // 暂不提供 queue_ms：发送侧 ts 与接收侧 arrival 在不同时钟域，
            // 相减得到的是「时钟差 + 真实时延」，其最小值也不是可靠基线
            // （实测在 0% 丢包下报出 371~497ms，直接把 ABR 打到最低档）。
            // 真正的排队估计需要时钟同步或探测帧往返，当前都没有，
            // 因此先按 0 上报，ABR 只用「丢包率 + 迟滞」决策。
            queue_ms: 0,
            frag_received: frag.fragments_received.min(u32::MAX as u64) as u32,
            frag_missing: frag.fragments_missing.min(u32::MAX as u64) as u32,
            datagram_loss_pct: {
                let dr = frag
                    .fragments_received
                    .saturating_sub(self.last_frag_received);
                let dm = frag
                    .fragments_missing
                    .saturating_sub(self.last_frag_missing);
                self.last_frag_received = frag.fragments_received;
                self.last_frag_missing = frag.fragments_missing;
                let total = dr + dm;
                if total == 0 {
                    0.0
                } else {
                    (dm as f32) * 100.0 / (total as f32)
                }
            },
        };

        // 新周期的基线：从本周期最后一帧之后开始算
        self.cycle_base_seq = None;
        self.cycle_received = 0;
        self.max_gap_ms = 0;
        Some(report)
    }

    pub fn cumulative(&self) -> (u32, u32) {
        let base = self.cum_base_seq.unwrap_or(0);
        let expected = if self.cum_received == 0 {
            0
        } else {
            self.cum_max_seq.saturating_sub(base) + 1
        };
        (expected, self.cum_received)
    }
}

/// 把 `QualityLevel` 摊成真正下发给编码器的参数。
///
/// 分辨率按 `downscale` 从**源尺寸**算，不按上一档算——否则连续降档会一直除下去，
/// 最后掉到 1/27 分辨率（这是阶梯式 ABR 最常见的实现 bug）。
pub fn encode_config_for(level: &QualityLevel, src_w: u32, src_h: u32) -> EncodeConfig {
    let d = level.downscale.max(1);
    EncodeConfig {
        width: (src_w / d) & !1,
        height: (src_h / d) & !1,
        fps: level.fps.max(1),
        bitrate_kbps: level.bitrate_kbps,
        gop: level.fps.max(1),
        bframes: 0,
    }
}

/// 发送侧 ABR 驱动：吃反馈、出配置变更。
#[derive(Debug)]
pub struct AbrDriver {
    ctrl: AbrController,
    src_w: u32,
    src_h: u32,
    applied: QualityLevel,
    feedbacks: u32,
    changes: u32,
}

impl AbrDriver {
    /// 诊断用：当前档位下标与健康/拥塞计数。
    pub fn debug_state(&self) -> (usize, u32, u32, u32) {
        (
            self.ctrl.index(),
            self.ctrl.healthy_streak(),
            self.ctrl.severe_streak(),
            self.ctrl.mild_streak(),
        )
    }

    pub fn new(src_w: u32, src_h: u32) -> Self {
        Self {
            ctrl: AbrController::new(0),
            src_w,
            src_h,
            applied: LADDER[0],
            feedbacks: 0,
            changes: 0,
        }
    }

    pub fn level(&self) -> QualityLevel {
        self.applied
    }

    pub fn config(&self) -> EncodeConfig {
        encode_config_for(&self.applied, self.src_w, self.src_h)
    }

    pub fn feedbacks(&self) -> u32 {
        self.feedbacks
    }

    pub fn changes(&self) -> u32 {
        self.changes
    }

    /// 吃掉一份报告。返回 Some(新配置) 表示档位变了，调用方要重配编码器。
    pub fn on_report(
        &mut self,
        report: &FeedbackReport,
        rtt_ms: u32,
    ) -> Option<(Decision, EncodeConfig)> {
        self.feedbacks += 1;
        let fb = report.to_link_feedback(rtt_ms);
        let d = self.ctrl.on_feedback(&fb);
        if !d.changed {
            return None;
        }
        self.applied = d.level;
        self.changes += 1;
        let cfg = encode_config_for(&d.level, self.src_w, self.src_h);
        Some((d, cfg))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hdr(seq: u32, ts: u32) -> VideoHeader {
        VideoHeader {
            seq,
            ts_ms: ts,
            width: 2880,
            height: 1620,
        }
    }

    #[test]
    fn video_header_roundtrip() {
        let h = hdr(7, 1234);
        let got = VideoHeader::decode(&h.encode()).expect("decode");
        assert_eq!(got, h);
        assert!(VideoHeader::decode(&[1, 2, 3]).is_none());
    }

    fn no_frag() -> ReassemblyStats {
        ReassemblyStats::default()
    }

    #[test]
    fn feedback_roundtrip() {
        let r = FeedbackReport {
            seq: 3,
            cycle_expected: 30,
            cycle_received: 27,
            cum_expected: 90,
            cum_received: 81,
            loss_pct: 10.0,
            echo_ts_ms: 999,
            rtt_hint_ms: 12,
            queue_ms: 40,
            frag_received: 900,
            frag_missing: 45,
            datagram_loss_pct: 4.76,
        };
        let got = FeedbackReport::decode(&r.encode()).expect("decode");
        assert_eq!(got, r);
    }

    #[test]
    fn loss_is_counted_from_seq_gaps() {
        let mut s = LinkStats::new();
        // 收到 seq 1,2,4,5 → 周期内应有 5 帧，实收 4 → 20%
        for (i, seq) in [1u32, 2, 4, 5].iter().enumerate() {
            s.on_frame(&hdr(*seq, 100 + i as u32), 1000 + i as u32 * 16);
        }
        let r = s.take_cycle(1100, no_frag()).expect("cycle");
        assert_eq!(r.cycle_expected, 5);
        assert_eq!(r.cycle_received, 4);
        assert!((r.loss_pct - 20.0).abs() < 0.001, "loss={}", r.loss_pct);
    }

    #[test]
    fn empty_cycle_reports_nothing() {
        let mut s = LinkStats::new();
        assert!(s.take_cycle(1000, no_frag()).is_none());
        s.on_frame(&hdr(1, 10), 1000);
        assert!(s.take_cycle(1000, no_frag()).is_some());
        assert!(
            s.take_cycle(1100, no_frag()).is_none(),
            "没新帧的周期不出报告"
        );
    }

    #[test]
    fn cumulative_counts_survive_cycles() {
        let mut s = LinkStats::new();
        for seq in 1..=20u32 {
            s.on_frame(&hdr(seq, seq), 1000 + seq * 16);
        }
        let r = s.take_cycle(1400, no_frag()).expect("cycle");
        assert_eq!((r.cum_expected, r.cum_received), (20, 20));
        for seq in 21..=30u32 {
            s.on_frame(&hdr(seq, seq), 2000 + seq * 16);
        }
        let r2 = s.take_cycle(2500, no_frag()).expect("cycle2");
        assert_eq!(r2.cycle_expected, 10);
        assert_eq!(r2.cum_received, 30);
    }

    /// 分片口径优先：帧丢失率 40%、分片丢失率 5% → ABR 必须看到 5%
    #[test]
    fn abr_uses_datagram_loss_not_frame_loss() {
        let r = FeedbackReport {
            seq: 1,
            cycle_expected: 10,
            cycle_received: 6,
            cum_expected: 10,
            cum_received: 6,
            loss_pct: 40.0,
            echo_ts_ms: 1,
            rtt_hint_ms: 0,
            queue_ms: 0,
            frag_received: 1140,
            frag_missing: 60,
            datagram_loss_pct: 5.0,
        };
        let fb = r.to_link_feedback(30);
        assert!(
            (fb.loss_pct - 5.0).abs() < 0.001,
            "应取分片口径，得到 {}",
            fb.loss_pct
        );
    }

    #[test]
    fn config_uses_source_dims_not_previous_level() {
        let src = (2880u32, 1620u32);
        let a = encode_config_for(&LADDER[0], src.0, src.1);
        assert_eq!((a.width, a.height), (2880, 1620));
        assert_eq!(a.bitrate_kbps, 8000);
        let b = encode_config_for(&LADDER[5], src.0, src.1);
        assert_eq!((b.width, b.height), (960, 540), "360p15 是源尺寸/3");
        assert_eq!(b.fps, 15);
    }

    /// 全链路：15% 丢包 → 档位必须降，且降够之后分辨率要变小
    ///
    /// 注意 ABR 有最小改档间隔（`min_interval_cycles`）：连降两档之后再降一档
    /// 需要多喂一份反馈，这里按真实节奏喂，不假设一报告一改档。
    #[test]
    fn driver_downgrades_on_loss() {
        let mut d = AbrDriver::new(2880, 1620);
        let start = d.config();
        let r = FeedbackReport {
            seq: 1,
            cycle_expected: 60,
            cycle_received: 51,
            cum_expected: 60,
            cum_received: 51,
            loss_pct: 15.0,
            echo_ts_ms: 100,
            rtt_hint_ms: 0,
            queue_ms: 0,
            frag_received: 850,
            frag_missing: 150,
            datagram_loss_pct: 15.0,
        };
        // PRD 5.2.4：严重拥塞单周期即降，首份报告就必须改档。
        let first = d
            .on_report(&r, 30)
            .expect("severe loss must downgrade on the first cycle");
        assert_eq!(first.0.level.name, "900p30");
        // 继续喂同一份（累积）报告，直到出现分辨率降档或档位不再变化
        let mut last_w = first.1.width;
        let mut downscaled = None;
        for _ in 0..8 {
            if let Some((decision, cfg)) = d.on_report(&r, 30) {
                last_w = cfg.width;
                if cfg.width < start.width {
                    downscaled = Some((decision.level.name, cfg));
                    break;
                }
            }
        }
        let (name, cfg) = downscaled.expect("连续拥塞必须降到更小分辨率");
        assert!(cfg.width < start.width, "分辨率必须降档: {name}");
        assert!(cfg.bitrate_kbps < start.bitrate_kbps, "码率必须降档");
        assert_eq!(cfg.fps, 30);
        assert!(last_w <= start.width);
    }

    #[test]
    fn driver_keeps_level_without_loss() {
        let mut d = AbrDriver::new(2880, 1620);
        for i in 0..10 {
            let r = FeedbackReport {
                seq: i,
                cycle_expected: 60,
                cycle_received: 60,
                cum_expected: 60,
                cum_received: 60,
                loss_pct: 0.0,
                echo_ts_ms: 100,
                rtt_hint_ms: 0,
                queue_ms: 0,
                frag_received: 960,
                frag_missing: 0,
                datagram_loss_pct: 0.0,
            };
            assert!(d.on_report(&r, 20).is_none(), "0% 丢包不得降档");
        }
        assert_eq!(d.level().name, "1080p60");
    }

    /// 低帧率（帧间隔 250ms）但零丢包时，queue 必须接近 0，
    /// 否则 ABR 会把"发得慢"误判成"网络排队"。
    #[test]
    fn low_fps_does_not_look_like_congestion() {
        let mut st = LinkStats::new();
        for i in 0..4u32 {
            let ts = i * 250;
            st.on_frame(
                &VideoHeader {
                    seq: i,
                    ts_ms: ts,
                    width: 960,
                    height: 540,
                },
                ts + 20,
            );
        }
        let fb = st
            .take_cycle(775, ReassemblyStats::default())
            .expect("report");
        assert_eq!(fb.loss_pct, 0.0);
        assert!(
            fb.queue_ms < 40,
            "250ms frame interval must not be reported as queue, got {}",
            fb.queue_ms
        );
    }
}
