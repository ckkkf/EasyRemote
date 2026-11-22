# ER-503 证据：QUIC 通道映射

- 状态: 部分完成（原生 QUIC；浏览器 WT 未实现）
- gate: PASS

## 1. 变更

| 内容 | 说明 |
|---|---|
| control_tx/data_tx | 控制 uni / 数据 bidi 各一队长驻流 |
| spawn_reader | accept_uni/accept_bi + 长度分帧 → mpsc |
| recv_reliable | 从 mpsc 收 Frame（保证控制顺序） |
| send_reliable | 按通道入独立队列；满 Backpressure |
| tests | 控制有序、数据背压不阻塞控制、端到端 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 视频灌包时控制仍有序 | control_ordered_under_video_flood | PASS |
| 数据队列满不阻塞控制 | data_backpressure_does_not_block_control | PASS |
| 既有数据报/可靠流 | quic_end_to_end | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 命令与输出

    $ cd core && cargo test -p er-transport quic::tests
    4 passed
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 边界

- 浏览器 WebTransport（wtransport/h3）未实现，ER-503 只完成原生 QUIC 通道语义。
- reader 任务每流一个 task；长会话多流需回收策略（ER-900）。
- 单帧 16MB 上限、队列 256 是固定值，需按 PRD 复核。
- 未做丢包/乱序下的控制顺序压力测试（ER-504/ER-903）。

## 5. 下一步

ER-503a：wtransport 浏览器适配（或明确降级到 WS/Relay），补浏览器握手/数据/控制测试；完成后才能宣称 WebTransport。
