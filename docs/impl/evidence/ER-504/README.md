# ER-504 证据：弱网 E2E

- 状态: 部分完成（5% 丢包通过；断网/重连未覆盖）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| e2e_services.dart | 构建/启动 lossyproxy；lossyRelayAddr；就绪读日志；停止回收 |
| lossy_e2e_test.dart | 经弱网中继跑完整会话 |

## 2. 参数

    -loss 0.05 -delay 2ms -jitter 2ms -reorder 0.02 -seed 7

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 5% 丢包/乱序下握手/加密/解码完成 | 弱网丢包下会话仍可建立 | PASS |
| link 可观测（p2p/relay） | 同上 | PASS |
| 既有 E2E 不回归 | host/session E2E | PASS |

## 4. 命令与输出

    $ cd client && flutter test --concurrency=1 test/lossy_e2e_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 未完成

- 拔网 5s、10s 内恢复（PRD 阈值）没有自动化；客户端无自动重连状态机。
- Relay 重启、旧 ticket 重放（已部分由 ER-205 覆盖）未在弱网 E2E 中验证。
- 无队列/线程泄漏长时检查。
- lossyproxy 只影响 UDP，不影响 HTTP/WS。

## 6. 下一步

ER-504a：断网/Relay 重启/自动重连 + 阈值断言；ER-900 长时资源检查。
