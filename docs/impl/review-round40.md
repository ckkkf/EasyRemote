# 第四十轮：ABR 闭环实测（0% 不误降，15% 真降档）

> 日期：2026-11-20 ｜ 主控 agent 真机执行

## 一、结果对照

| 注入丢包 | 档位变化 | 最终档位 | 码率 | 帧率 | 输出分辨率 | 零拷贝 |
|---|---|---|---|---|---|---|
| **0%** | changes=**0** | **1080p60** | 8000 kbps | 60 | 2880×1620 | gpu-zero-copy |
| **15%** | changes=**4** | **360p15** | 800 kbps | 15 | 960×540 | cpu-scaled |

原始摘要：

```
ABR_TX_SUMMARY: frames_sent=90 bytes_total=1111844 feedbacks=22 changes=0
  final_level=1080p60 final_bitrate_kbps=8000 final_fps=60 final_dims=2880x1620
  backend=gpu-zero-copy rtt_min=0ms rtt_max=0ms

ABR_TX_SUMMARY: frames_sent=82 bytes_total=739662 feedbacks=22 changes=4
  final_level=360p15 final_bitrate_kbps=800 final_fps=15 final_dims=960x540
  backend=cpu-scaled rtt_min=0ms rtt_max=0ms
```

**零丢包时完全不动，真丢包时连降两档。** 这是 ABR 第一次在真实链路上产生符合预期的行为。

## 二、这一轮修的三个缺陷

### 1. queue_ms 把「帧间隔」当「排队」

`queue_ms = max_gap_ms.max(arrival - last_arrival)`。低帧率下帧间隔天然很大
（1s 周期 4 帧 → 250ms），被当成严重拥塞 → 降档 → 帧率更低 → 间隔更大 → **死循环降到最低档**。

实测 0% 丢包时报出 371~497ms 的「排队」。

**处理**：queue_ms 先上报 0，ABR 只用「丢包率 + 迟滞」决策。
理由：发送侧 ts 与接收侧 arrival 在不同时钟域，相减得到的是「时钟差 + 真实时延」，
其最小值也不是可靠基线。真正的排队估计需要时钟同步或探测帧往返，当前都没有。

### 2. RTT 跨时钟域相减

`rtt_ms = now_ms() - report.echo_ts_ms` —— echo_ts 是接收侧到达时间戳，
now_ms 是发送侧单调时钟。实测 67~752ms 乱跳，ABR 把它当拥塞。

**处理**：改用 `report.rtt_hint_ms`（未提供时为 0，即不按 RTT 降档），并注明真实 RTT 需要探测帧往返。

### 3. 单周期假丢包就降两档

周期边界上偶尔丢一帧（`cycle_expected=4 cycle_received=2` → 50% 假丢包）就触发 severe。

**处理**：`AbrController` 增加迟滞——severe 需连续 2 周期、mild 需连续 2 周期。
新增单测：
- `single_cycle_spike_does_not_downgrade`（单周期 50% 假丢包不降档）
- `sustained_congestion_downgrades`（连续两周期才降）

原先 4 条「单周期即降档」的用例按新语义更新。

## 三、测试

```
cargo test -p er-abr        → 11 passed
cargo test -p er-client     → 24 passed
```

## 四、闸门状态（如实记录）

```
bash scripts/gate.sh          → GATE: PASS（报告模式，7 处待迁移）
bash scripts/gate.sh --strict → GATE: FAIL
  违规 ADM-01/02/04/05/07/09/09-noauth：响应缺字段 code
```

这 7 处是**既有历史债**（admin 后台响应未走 `httpx.OK` 信封），不是本轮引入。
按 AGENTS.md 第 4 条，`--strict` 绿了才算真正对齐——**这条目前没做到，记为未完成**。

## 五、仍未闭合

| 项 | 状态 |
|---|---|
| ABR 按 queue/RTT 降档 | ❌ 依赖时钟同步，当前按 0 上报 |
| ABR 恢复升档实测 | ⏳ 未测（只在单测里覆盖 healthy_streak） |
| ACCESS_LOST 自愈 | ⚠️ INCONCLUSIVE |
| gate --strict 全绿 | ❌ 7 处 admin 信封待迁移 |
