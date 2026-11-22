# ER-610 证据：输入有序与 ACK

- 状态: 部分完成（有序/去重/ACK/P95；端到端门控未测）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-input | InputSequencer（有序/去重/64 窗口） |
| agent | MSG_INPUT seq；MSG_INPUT_ACK；ControllerAgent input_seq |
| host_stream | 按序注入 + ACK |
| ctrl_session | pending seq→时间；P95；input_acks |
| er-ffi/Dart/UI | input_p95_ms/input_acks 展示 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 乱序按序交付 | out_of_order_is_delivered_in_order | PASS |
| 重复丢弃 | duplicates_are_dropped | PASS |
| 缓存有界 | pending_is_bounded | PASS |
| P95 窗口 | p95_is_window_index | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- closed/rejected 后输入 100% 无效：代码依赖会话循环退出，缺端到端断言。
- 真实弱网 P95 证据未采集。
- ACK 丢失无重传；P95 仅统计收到的 ACK。
- 坐标映射依赖 UI 已解码帧宽高；纹理路径尚未提供尺寸（ER-601b）。

## 4. 下一步

ER-610a：closed 后输入门控端到端测试、ACK 丢失/重传、真实弱网 P95；修纹理路径坐标尺寸。
