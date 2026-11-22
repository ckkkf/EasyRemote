# ER-502a 证据：候选交换编码

- 状态: 部分完成（编解码+校验；未接线）
- gate: PASS

## 1. 产出

| 内容 | 说明 |
|---|---|
| CANDIDATE_MSG_MAGIC / encode / decode | Relay 上交换候选的二进制格式 |
| candidate_exchange_roundtrip_and_token_check | roundtrip + 错误 token/session 拒绝 |

## 2. 格式

    magic(7) | token(16) | sidLen(u16) | sid | count(u8) | [kind(1) ip(4) port(2)]*

- 候选最多 16；仅 IPv4；解析越界/超限返回 None。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 候选列表可往返 | candidate_exchange_roundtrip_and_token_check | PASS |
| 错误 token/session 被拒 | 同上 | PASS |

## 4. 命令与输出

    $ cd core && cargo test -p er-transport p2p
    4 passed
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 未完成

- pair/conductor 未在注册后发送/接收该消息。
- 无两阶段 ready（双方打洞成功后确认切换）与 send/recv 链路选择。
- 无 link_type 指标与“P2P 失败自动 Relay”会话级测试。
- token 目前由调用方提供；接入需来自会话认证上下文。

## 6. 下一步

ER-502b：RelayClient 增加 candidate exchange / ready / link 选择；pair 与会话编排接线；E2E 覆盖成功与失败切换。
