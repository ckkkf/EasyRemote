//! D1 网络诊断：把探测数据翻译成「为什么卡、该怎么办」。

/// NAT 类型。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NatType {
    /// 完全锥形：最容易打洞
    Cone,
    /// 地址受限锥形
    RestrictedCone,
    /// 端口受限锥形
    PortRestricted,
    /// 对称型：几乎必然打洞失败
    Symmetric,
    Unknown,
}

impl NatType {
    pub fn label(self) -> &'static str {
        match self {
            NatType::Cone => "完全锥形",
            NatType::RestrictedCone => "地址受限锥形",
            NatType::PortRestricted => "端口受限锥形",
            NatType::Symmetric => "对称型",
            NatType::Unknown => "未知",
        }
    }

    /// 是否容易打洞成功。
    pub fn is_punch_friendly(self) -> bool {
        matches!(self, NatType::Cone | NatType::RestrictedCone | NatType::PortRestricted)
    }
}

/// 探测数据。
#[derive(Debug, Clone)]
pub struct ProbeReport {
    pub local_nat: NatType,
    pub peer_nat: NatType,
    /// 打洞尝试次数
    pub punch_attempts: u32,
    pub punch_succeeded: bool,
    /// 走中继时的额外延迟（毫秒）
    pub relay_extra_rtt_ms: u32,
    /// UDP 是否被阻断
    pub udp_blocked: bool,
    /// 是否疑似被运营商限速
    pub qos_suspected: bool,
    /// 实测丢包率（百分比）
    pub loss_pct: f32,
}

/// 诊断出的原因。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Cause {
    DirectConnected,
    SymmetricNat,
    PunchFailed,
    UdpBlocked,
    QosSuspected,
    HighLoss,
}

/// 诊断结论。
#[derive(Debug, Clone, PartialEq)]
pub struct Diagnosis {
    /// 一句话结论（给用户看）
    pub verdict: String,
    pub causes: Vec<Cause>,
    /// 影响描述
    pub impact: String,
    /// 建议（可执行）
    pub advice: Vec<String>,
}

/// 把探测结果翻译成人话。
pub fn diagnose(r: &ProbeReport) -> Diagnosis {
    let mut causes = Vec::new();
    let mut advice = Vec::new();

    if r.punch_succeeded {
        causes.push(Cause::DirectConnected);
        let mut impact = "延迟与码率均不受中继限制".to_string();
        if r.loss_pct >= 5.0 {
            causes.push(Cause::HighLoss);
            impact = format!("丢包 {:.1}%，画面可能降档", r.loss_pct);
            advice.push("优先使用有线网络或 5GHz Wi-Fi".to_string());
        }
        return Diagnosis {
            verdict: "P2P 直连成功，链路正常".to_string(),
            causes,
            impact,
            advice,
        };
    }

    // 打洞失败：给出可解释的原因
    if r.udp_blocked {
        causes.push(Cause::UdpBlocked);
        advice.push("当前网络阻断了 UDP，请尝试更换网络或放行 UDP 443".to_string());
    }
    if r.local_nat == NatType::Symmetric || r.peer_nat == NatType::Symmetric {
        causes.push(Cause::SymmetricNat);
        let who = if r.local_nat == NatType::Symmetric { "你的" } else { "对方的" };
        advice.push(format!(
            "{who}网络为对称型 NAT，与多数网络无法直连；如在公司网络可联系网管放行 UDP 443/3478"
        ));
    }
    causes.push(Cause::PunchFailed);
    if r.qos_suspected {
        causes.push(Cause::QosSuspected);
        advice.push("检测到疑似运营商限速，建议错峰使用".to_string());
    }

    let verdict = if r.qos_suspected {
        "已切换中继，且检测到疑似运营商限速".to_string()
    } else {
        format!("P2P 打洞失败（尝试 {} 次），已切换中继转发", r.punch_attempts)
    };

    let impact = format!(
        "延迟增加约 {} ms，码率上限受限；画面可能比直连更模糊",
        r.relay_extra_rtt_ms
    );

    if advice.is_empty() {
        advice.push("如长期如此，可尝试更换网络环境".to_string());
    }

    Diagnosis { verdict, causes, impact, advice }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn base() -> ProbeReport {
        ProbeReport {
            local_nat: NatType::PortRestricted,
            peer_nat: NatType::Cone,
            punch_attempts: 3,
            punch_succeeded: false,
            relay_extra_rtt_ms: 12,
            udp_blocked: false,
            qos_suspected: false,
            loss_pct: 0.0,
        }
    }

    #[test]
    fn direct_connection_reports_healthy() {
        let mut r = base();
        r.punch_succeeded = true;
        let d = diagnose(&r);
        assert!(d.verdict.contains("直连"));
        assert!(d.causes.contains(&Cause::DirectConnected));
        assert!(d.advice.is_empty());
    }

    #[test]
    fn symmetric_nat_explains_why() {
        let mut r = base();
        r.local_nat = NatType::Symmetric;
        let d = diagnose(&r);
        assert!(d.causes.contains(&Cause::SymmetricNat));
        assert!(d.verdict.contains("中继"));
        assert!(d.advice.iter().any(|a| a.contains("UDP 443")));
    }

    #[test]
    fn udp_blocked_gives_actionable_advice() {
        let mut r = base();
        r.udp_blocked = true;
        let d = diagnose(&r);
        assert!(d.causes.contains(&Cause::UdpBlocked));
        assert!(d.advice.iter().any(|a| a.contains("UDP")));
    }

    #[test]
    fn qos_mention_off_peak() {
        let mut r = base();
        r.qos_suspected = true;
        let d = diagnose(&r);
        assert!(d.causes.contains(&Cause::QosSuspected));
        assert!(d.advice.iter().any(|a| a.contains("错峰")));
    }

    #[test]
    fn high_loss_on_direct_connection_is_called_out() {
        let mut r = base();
        r.punch_succeeded = true;
        r.loss_pct = 8.0;
        let d = diagnose(&r);
        assert!(d.causes.contains(&Cause::HighLoss));
        assert!(d.impact.contains("丢包"));
    }

    #[test]
    fn impact_mentions_relay_latency() {
        let d = diagnose(&base());
        assert!(d.impact.contains("12"));
    }

    #[test]
    fn only_symmetric_blocks_punching() {
        assert!(NatType::Cone.is_punch_friendly());
        assert!(!NatType::Symmetric.is_punch_friendly());
        assert_eq!(NatType::Symmetric.label(), "对称型");
    }
}
