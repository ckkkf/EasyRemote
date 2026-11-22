# ER-400 证据：统一 RuntimeConfig

- 状态: 完成（端点下发 + 无硬编码扫描；gate PASS）
- 限制：Rust 未自行拉配置；STUN/WT 尚未消费。

## 1. 变更

| 层 | 内容 |
|---|---|
| 契约 | RuntimeEndpoints；ClientRuntimeConfig.endpoints |
| 服务端 | RuntimeEndpointsFromEnv：开发默认、生产 HTTPS/localhost 校验 |
| Flutter | GatewayClient.loadRuntimeConfig；gatewayURL/signalURL/relayAddr；main 启动加载 |
| 页面 | session/device 用运行时端点；空 relay 明确失败；self-test 无默认地址 |
| 扫描 | scripts/tests/runtime-config-hardcode.test.mjs |

## 2. 语义

- /v1/config 无需登录，返回 endpoints + limits + capabilities。
- 生产：ER_PUBLIC_* 必须显式 https，TLS=1，禁止 localhost。
- 开发：gateway 默认 localhost:8080，signal 同源，relay 默认本机。
- 页面：relay 空 → 不启动被控/画面并给出错误；不静默回退。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 开发默认端点一致 | TestRuntimeEndpointsDevDefaults | PASS |
| 生产拒绝 localhost/http | TestRuntimeEndpointsProductionRequiresHTTPS | PASS |
| 生产显式 https 通过 | TestRuntimeEndpointsProductionExplicit | PASS |
| 发布代码无硬编码本机端点 | runtime-config-hardcode.test.mjs | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/appconfig/
    PASS
    $ node --test scripts/tests/runtime-config-hardcode.test.mjs
    pass 1
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- Rust er-client 仍由 FFI/调用方传入 gateway/relay；未拉取 /v1/config（后续卡）。
- STUN/WebTransport/TLS 字段已下发但客户端未使用；P2P/HTTP3 在 Phase 5。
- Flutter Web 默认相对路径不受影响；桌面必须 --dart-define=ER_GATEWAY。
- 无 endpoint 的旧服务端会让客户端启动时 loadRuntimeConfig 失败并记录，不阻塞登录（登录仍用 baseUrl）。

## 6. 回滚

- 回滚端点下发会恢复客户端硬编码，禁止。
- 生产校验不得放宽；本地开发默认不能进入发布。
