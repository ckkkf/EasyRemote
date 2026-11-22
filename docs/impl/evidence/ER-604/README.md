# ER-604 证据：ABR 输入扩展

- 状态: 部分完成（吞吐/编码队列未实测）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-abr | LinkFeedback 新字段；severe/mild/healthy 规则 |
| er-client/link | buffer=queue 代理，吞吐/编码队列=0 未知 |
| er-ffi | er_abr_update 填 queue/buffer |
| tests | 低吞吐、缓冲、2000 步属性 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 吞吐低于当前档 90% 不升档 | low_throughput_blocks_upgrade | PASS |
| buffer>300 参与严重降档 | buffer_and_encode_queue_are_congestion | PASS |
| 单次降≤2/升≤1/索引不越界 | property_decisions_stay_in_ladder | PASS |
| 既有 ABR 用例 | er-abr 16 条 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- 反馈报文没有吞吐/编码队列字段；当前 throughput=0、encode=0（未知），buffer 用 queue 代理。
- 没有真实链路吞吐测量驱动 ABR。
- 无 UI 展示 ABR 档位/原因。

## 4. 下一步

ER-604a：扩展 FeedbackReport 携带实测吞吐/编码队列，并在发送端按 ABR 档位重配编码器；补真实链路升降档 E2E。
