# ER-205 证据：重连与票据轮换

- 状态: 完成（授权复用 + 撤销 + 代号轮换；gate PASS）
- 限制：拔网恢复 E2E 与客户端自动重连 UI 归 ER-903/ER-900。

## 1. 变更

| 层 | 内容 |
|---|---|
| relaymgr | Ticket.Epoch；IssueWithEpoch；Verify 兼容 4/5 字段 |
| signal RedisBus | NextTicketEpoch（INCR）/ TicketEpoch / 撤销键 |
| gateway | App.TicketEpochs；票据端点每次换代号；后端失败拒绝签票 |
| relay | RedisRegistry 代号读写；TicketEpochCurrent；注册时先撤销后代号 |
| session | Authorized 窗口内复用（已有 Request 逻辑，补测试） |

## 2. 语义

- 重连：Request 返回未过期 authorized，无需重新授权。
- 换票：每次领取票据 epoch+1；旧 epoch 在中继注册时被拒。
- 关闭：撤销键优先，任何 epoch 都不能再注册。
- 单机/无 Redis：epoch=0，不轮换（不得用于生产）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 5 分钟后重连复用 authorized | TestAuthorizedSessionReusedWithinWindow | PASS |
| 关闭后旧票据被撤销键拒绝 | TestRevokedTicketRejectedAfterClose | PASS |
| 换票后旧代号被拒 | TestStaleTicketRejectedAfterRotation | PASS |
| 既有票据/中继测试 | relaymgr/relay 套件 | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/relay/ -run 'TestRevoked|TestStale' -v
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 没有真实拔网 5s、10s 内恢复的 E2E；需要网络故障注入（ER-903）。
- 客户端没有“自动重连”状态机/UI；FFI attach 存在但 Flutter 未接。
- Redis 不可用时中继拒绝带代号票据（fail-closed），可能造成可用性下降；监控归 ER-903。
- epoch=0 票据仍被接受（兼容旧客户端）；生产应从网关签发保证 >0。

## 6. 回滚

- 回滚代号检查会允许旧票重放，禁止。
- 回滚授权复用会让重连重新弹授权，影响体验但非安全问题。
