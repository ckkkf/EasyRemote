# 第二十八轮：断线重连（退避策略 + 真实掐断验证）

> 日期：2026-11-20 ｜ 目标轮次 19/20

## 一、退避策略（PRD §5.1.3）

`er-session::backoff::BackoffPolicy`：

```rust
BackoffPolicy { base_ms: 1000, factor: 2, max_ms: 60_000, max_attempts: 0 }
```

实测序列与 PRD 一致：

```
1s → 2s → 4s → 8s → 16s → 32s → 60s → 60s
```

6 条单测覆盖：序列正确、上限夹取、次数限制、0 基准不等待、单调不减。

## 二、真实掐断验证（不是模拟）

```
ok: initial exchange works
ok: exchange fails while relay is down
ok: reconnected after 1 retries with backoff
RECONNECT E2E PASSED
```

流程：

1. 起真实 Go 中继 → 两端注册 → 收发成功
2. **杀掉中继进程** → 收发确实失败（这条断言很关键：证明第 3 步不是假成功）
3. 重启中继 → 客户端按 200ms/400ms/800ms… 退避重新注册 → 恢复收发

第 2 条那种「先证明它真的坏了」的断言，是避免"重连测试永远通过"的关键。

## 三、为什么重连必须重新注册

中继的端位表是进程内状态，重启后清空。客户端光「继续发包」没用——
必须重新 `register` 才能恢复双向转发。这正是 `retry_until` 循环里做的事。

## 四、验证

| 端 | 结果 |
|---|---|
| Rust | **134 passed**（128 + 6） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | 18 passed |
| 断线重连端到端 | 通过（真杀进程） |

## 五、还没做的

| 缺口 | 说明 |
|---|---|
| 会话级重连 | 现在验证的是中继链路层；会话状态（谁的会话、加密密钥）跨重连的恢复未做 |
| ICE 打洞 | 强制中继，需要 STUN/TURN 环境 |
| 硬件编解码 | 软件 95ms/帧，离 40ms 有距离 |
| Windows 注入 | `SendInput` 未实现 |
