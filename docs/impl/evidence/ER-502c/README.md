# ER-502c 证据：生产 P2P 接入

- 状态: 完成（代码接入 + link 可观测；独立进程 E2E 未做）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-session/relay_client.rs | negotiate_p2p；p2p_token 公开 |
| er-client/host_stream.rs | begin 后协商；stats.link |
| er-client/ctrl_session.rs | 两处 begin 后协商 |
| er-ffi/host.rs | status JSON 增加 link |
| client/host_service_io.dart | HostStatus.link |

## 2. 语义

- 独立进程按 prepare→await→punch→ready→activate 顺序协商；双方成功才 P2P。
- 失败/超时保持 Relay；握手和加密路径不变。
- 被控状态 link 可读（p2p/relay）。

## 3. 验收

| 断言 | 测试/证据 | 结果 |
|---|---|---|
| 既有会话/主机 E2E 不回归 | host_link / session_e2e | PASS |
| 生产代码调用协商 | 代码路径 + clippy | PASS |
| link 可观测 | FFI status/Dart 字段 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 4. 命令与输出

    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 未完成/风险

- 没有独立进程 host_stream/ctrl_session 的 P2P 成功/失败自动化断言；host_link 用的 conductor 一次性流程也未接 P2P。
- NAT/对称 NAT、Relay 强制回退、链路切换时延未测（ER-903）。
- token 由 session_id 派生，生产需会话密钥派生。
- 协商固定 2s 等待，可能拉长建立时间；失败时多等 2s。

## 6. 回滚

- 回滚生产协商会回到全 Relay（可用，但 P2P 失效）。
- 不得只在一端切 P2P。
