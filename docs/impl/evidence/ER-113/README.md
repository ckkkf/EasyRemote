# ER-113 证据：WebSocket 安全边界

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 行为

| 项 | 规则 |
|---|---|
| Origin | ER_ALLOWED_ORIGINS 精确匹配；未配置只允许 localhost/127.0.0.1/::1；空 Origin 允许（原生客户端） |
| 读上限 | 64KB（signal.MaxWSMessageBytes） |
| 认证 deadline | 默认 5s；ER_WS_AUTH_TIMEOUT 可覆盖（测试 150ms） |
| 失败日志 | upgrade 失败记录 remote/path/err（ER-110 已加） |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 非法 Origin 在 upgrade 阶段 403 | TestRendezvousRejectsBadOrigin | PASS |
| 超过 64KB 消息断开 | TestRendezvousRejectsOversizedMessage | PASS |
| 未认证连接在 deadline 后断开 | TestRendezvousAuthDeadline | PASS |
| 既有正向认证/未授权拒绝 | TestRendezvousAcceptsRegisteredDevice / RejectsUnauthenticated | PASS |

## 3. 命令与输出

    $ cd server && go test ./internal/stack/ -run TestRendezvous -v
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 边界

- 生产必须配置 ER_ALLOWED_ORIGINS 才能给浏览器客户端使用；未配置时只放行本机 Origin，属于 fail-closed。
- 空 Origin 允许是有意为之：Rust/桌面原生不发送 Origin；若未来浏览器可伪造空 Origin 需在网关层再校验。
- cmd/rendezvous 的 upgrader 在进程启动时读取 env；stack 在构建 handler 时读取。
- 认证 deadline 只覆盖 HELLO 阶段；认证后仍有 60s 读超时（心跳续）。

## 5. 回滚

- 回滚 Origin allowlist 会恢复 CheckOrigin=true（A-24），禁止。
- 回滚读上限/认证 deadline 会重新暴露内存与慢速认证攻击，禁止。
