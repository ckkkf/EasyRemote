# 第四十二轮：ABR 单向棘轮修复（降档后能升回顶档）

> 日期：2026-11-20 ｜ 主控 agent 真机执行

## 一、问题：ABR 只会降，不会升

上一轮实测：15% 丢包把档位打到 360p15，把丢包切回 0% 后**连续 40 个周期都没有升档**。

调试打印（每 5 个反馈打一次内部状态）：

```
ABR_TX_STATE fb=55 idx=5 healthy=0 severe_s=0 mild_s=38 loss=0.0% q=0 rtt=0
```

链路已经彻底健康（帧丢失 0%、队列 0、RTT 0），但 `healthy` 永远是 0、`mild_s` 一路涨——
说明喂给 ABR 的 `loss_pct` 不是 0。

## 二、根因：分片丢包率用的是**累计值**

`FeedbackReport::to_link_feedback` 里：

```rust
let loss_pct = if frag_total > 0 { self.datagram_loss_pct } else { self.loss_pct };
```

而 `datagram_loss_pct` 来自 `frag.loss_pct()`，是**累计**统计。链路恢复后它仍停在 2~10%，
永远不会降下来 → `mild = loss > 2.0` 恒为真 → 永远不 healthy → **单向棘轮**。

## 三、修复：改成「本周期增量」

`LinkStats` 记录上一周期的分片累计数，`take_cycle` 里用差分算本周期的丢包率：

```rust
let dr = frag.fragments_received.saturating_sub(self.last_frag_received);
let dm = frag.fragments_missing.saturating_sub(self.last_frag_missing);
self.last_frag_received = frag.fragments_received;
self.last_frag_missing = frag.fragments_missing;
```

另外把健康计数从「清零」改成「漏桶」：单周期噪声只削一格，不清零。

## 四、真机结果：完整恢复到顶档

丢包 15% 起手，会话中途切到 0%：

```
540p30 -> 720p30   reason=链路健康，升一档
720p30 -> 360p15   reason=丢包/积压严重，连降两档     ← 切换瞬间还有残留在途丢包
360p15 -> 540p30   reason=链路健康，升一档
540p30 -> 720p30   reason=链路健康，升一档
720p30 -> 900p30   reason=链路健康，升一档
900p30 -> 1080p30  reason=链路健康，升一档
1080p30 -> 1080p60 reason=链路健康，升一档
```

收尾状态：`idx=0 healthy=21 mild_s=0` —— 回到最高档 1080p60 并稳定保持。

**ABR 现在是完整的双向闭环：拥塞降档、恢复升档、且升到顶后停止。**

## 五、回归

| 端 | 结果 |
|---|---|
| gate.sh | **PASS** |
| Rust | **162 passed** |
| Go 单测 / 竞态 | 19 / 19 全绿 |
| Flutter | 28 passed |

## 六、仍需注意

1. 切换瞬间仍会误降一次（在途丢包造成的假信号），属于可接受的瞬态
2. 升级是逐档爬，从 360p15 回 1080p60 需要约 12 个周期（≈12s），有优化空间但行为正确
3. 诊断打印 `ABR_TX_STATE` 暂时保留（每 5 个反馈一行），后续可加开关
4. `disable_debug`：`AbrDriver::debug_state()` 目前是公开方法，生产可加 `#[cfg(debug_assertions)]`
