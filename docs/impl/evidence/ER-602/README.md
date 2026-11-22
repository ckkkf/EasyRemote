# ER-602 证据：关键帧请求与有界重组

- 状态: 完成（关键帧/有界；30 分钟稳定性未做）
- gate: PASS

## 1. 变更

| 内容 | 说明 |
|---|---|
| MSG_KEYFRAME=5 | 主控→被控关键帧请求 |
| should_request_keyframe | seq 跳变 + ≥500ms 节流 |
| 解码失败 | 同样请求关键帧（节流） |
| host_stream | 收到请求后 enc.request_keyframe |
| Reassembler.inflight_len | 测试在途上限 8 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 丢包跳变请求、连续帧不请求、节流 | requests_on_gap_but_throttles | PASS |
| 在途消息有界 | inflight_messages_are_bounded | PASS |
| 既有 E2E | host/session/lossy | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- 30 分钟无累积延迟/内存稳定未跑。
- 抖动缓冲/重排策略未实现（当前只按 seq 请求关键帧）。
- 真实丢包恢复画面时间未量化。
- 关键帧请求频率固定 500ms，未按 RTT 自适应。

## 4. 下一步

ER-603：真实质量指标（FPS/码率/延迟/丢包）从媒体链路采集并在 UI/后台展示；ER-903 做长时稳定性。
