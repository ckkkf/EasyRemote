//! ABR 决策曲线：把弱网实测的链路反馈喂进纯逻辑控制器，看它会怎么决策。
//!
//! 注意：这是**离线仿真**，不是"ABR 在会话里被触发了"。
//! 当前数据面（er-client agent 的中继收发）没有把接收侧丢包反馈回发送侧，
//! 所以真实会话里 ABR 根本没被驱动；这个例子只回答"如果接上会怎样"。

use er_abr::{AbrController, LinkFeedback};

fn main() {
    println!(
        "{:<8} {:<10} {:>10} {:>8} {:>10} {}",
        "loss%", "rtt(ms)", "queue(ms)", "level", "bitrate", "changed/reason"
    );
    for &(loss, rtt, queue) in &[
        (0.0f32, 70u32, 0u32),
        (5.0, 70, 0),
        (15.0, 70, 0),
        (15.0, 300, 250),
    ] {
        let mut ctl = AbrController::new(0);
        // 连续喂 3 个周期，观察档位收敛（升档需要健康连续 3 周期）
        let mut last = ctl.current();
        let mut changed_any = false;
        let mut reason = "";
        for _ in 0..3 {
            let d = ctl.on_feedback(&LinkFeedback {
                loss_pct: loss,
                rtt_ms: rtt,
                queue_ms: queue,
            });
            if d.changed {
                changed_any = true;
            }
            reason = d.reason;
            last = d.level;
        }
        println!(
            "{:<8.1} {:<10} {:>10} {:>8} {:>10} {} / {}",
            loss, rtt, queue, last.name, last.bitrate_kbps, changed_any, reason
        );
    }
    println!("NOTE: ABR 在真实会话中未被驱动（数据面无丢包反馈回路），以上只是离线策略曲线。");
}
