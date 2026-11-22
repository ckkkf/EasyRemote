# ER-202 证据：关闭传播与票据撤销

- 状态: 完成（strict gate PASS）；限制：关闭时序缺专门 E2E，见第 5 节。

## 1. 变更

| 层 | 内容 |
|---|---|
| 网关 | GW-25 POST /v1/sessions/close；会话方校验；Close + Closer |
| 后台 | adminapi.SetSessionCloser；KillSession 关闭后传播 |
| 信令 | NotifySessionClosed：双方 DISCONNECT + DeleteBinding + RevokeSession |
| 中继 | RedisRegistry.IsRevoked；注册票据前检查 er:revoked:session:<id> |
| 客户端 | host_stream/ctrl_session 每 1s 读会话状态，closed/failed/ended 立即停止 |

## 2. 状态与传播

- close：任意会话方 → closed（reason/end），重复 close 幂等。
- 传播：DISCONNECT 投递给 controller/target；绑定删除；撤销键 TTL 2h。
- 旧票据：relay 注册时 IsRevoked=true 直接丢弃。
- 后台中断：先写 closed/audit，再传播；传播失败返回 SRV_INTERNAL（状态不回滚）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 一端关闭写 closed 并传播双方 | TestClosePropagatesToBothPeers | PASS |
| 后台强制中断传播 | TestAdminKillPropagatesClose | PASS |
| 关闭撤销票据 + 双方 DISCONNECT | TestConsentNotifierRevokesOnClose | PASS |
| 跨实例撤销键 + 绑定删除 | TestConsentNotifierCrossesSignalInstances | PASS |
| 既有 E2E | flutter test --concurrency=1（gate） | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/signal/ -run TestConsentNotifier -v
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 无 Redis 的开发中继不检查撤销键；生产必须 Redis（ER-411 配置一致性）。
- 3 秒内资源归零由 1s 状态轮询 + 循环退出保证，尚无“关闭后帧/输入计数归零”的 E2E 断言，归 ER-900。
- 关闭事件未写 outbox/审计（ER-203）。
- DISCONNECT 在 host 流期间依赖状态轮询，因为 host_stream 未常驻信令 WS；后续可改为事件推送。

## 6. 回滚

- 回滚传播会恢复“只有数据库 closed、双方继续推流”，禁止。
- 回滚撤销键会让旧票据在 1h TTL 内继续可用，禁止。
