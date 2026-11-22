# ER-201 证据：双方 ready 后才 active

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 变更

| 层 | 内容 |
|---|---|
| 存储 | ready_controller / ready_target 列；Memory/PG 读写；迁移 IF NOT EXISTS |
| 状态机 | CommandReady：单方 ready 保持 authorized；双方 ready 且窗口有效 → active；重复幂等；非会话方 403；窗口过期关闭 |
| HTTP | GW-24 POST /v1/sessions/ready（device）；SessionStateResult |
| Rust | Gateway.mark_ready；TargetAgent/ControllerAgent 封装；host_stream/conductor/ctrl_session 在 finish 后上报 |

## 2. 状态语义

| 当前 | 事件 | 结果 |
|---|---|---|
| authorized | controller ready | authorized（ReadyController=true） |
| authorized | target ready | active（双方就绪） |
| authorized | ready 时窗口过期 | closed(auth_window_exceeded) + 错误 |
| active | 重复 ready | 幂等，version 不变 |
| requested | ready | SESSION_NOT_FOUND |
| 任意 | 非会话方 ready | ADMIN_FORBIDDEN |

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 单方 ready 不激活；双方 ready 才 active | TestSessionActiveOnlyAfterBothPeersReady（session） | PASS |
| HTTP 端点同样语义 | TestSessionActiveOnlyAfterBothPeersReady（gateway） | PASS |
| 非会话方不能 ready | TestReadyRejectsOutsider / HTTP 403 | PASS |
| 窗口过期 ready 收口 | TestReadyAfterAuthWindowFails | PASS |
| 既有状态机/并发/幂等 | ER-200 测试 | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/session/ ./internal/gateway/
    ok
    $ bash scripts/gate.sh   # 含 go test -race + E2E
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 若一端 ready 后进程崩溃，会话停在 authorized，靠授权窗口/主动关闭收敛（ER-204/ER-202）。
- active 的关闭传播、双方资源停止仍归 ER-202。
- 路由 GW-24 已登记；方法/鉴权由 RouteSpec + MethodGuard 校验。

## 6. 回滚

- 回滚 ready 会让 authorized 重新被当作 active，禁止。
- 回滚 ready 列需保留 DEFAULT false 兼容旧数据。
