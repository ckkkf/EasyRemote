# ER-504a 证据：Relay 重启恢复

- 状态: 部分完成（注册层恢复；会话级未做）
- gate: PASS

## 1. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| Relay 300ms 后同端口重启，客户端重试后收到 ACK | register_recovers_after_relay_restart | PASS |
| 既有注册 ACK/失败 | ER-501 测试 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 2. 语义

- 客户端 register 每次等 ACK；失败按 BackoffPolicy 退避，总 deadline 5s。
- 测试用 fast 策略（10ms 起）避免长时间等待。
- 恢复判定严格依赖 ACK。

## 3. 命令与输出

    $ cd core && cargo test -p er-session register_recovers
    1 passed
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 未完成

- 会话级断网 5s/10s 自动重连（客户端没有 reconnect 状态机/UI）。
- Relay 重启后旧会话/旧票据/旧输入行为未验证。
- 没有长时资源/线程泄漏检查。

## 5. 下一步

ER-504b：会话级自动重连（attach/resume + 5s/10s 阈值 + 旧票据失效），或明确降级为手动重连并在 UI 文案标注。
