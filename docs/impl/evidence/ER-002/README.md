# ER-002 证据：自动启动真实 E2E 环境，禁止缺 env skip

- 状态: 完成（`bash scripts/gate.sh` → GATE: PASS；Flutter 62 passed）
- 日期: 2026-11-22

## 1. 交付

| 文件 | 目的 |
|---|---|
| `client/test/support/e2e_services.dart` | 构建/启动 stack+relay（随机端口）、/healthz 与 relay 日志就绪、/debug/presence 等被控上线、SIGKILL+有界等待回收；支持外部服务模式 |
| `client/test/e2e_services_test.dart` | `TestE2EHarnessStartsAndStopsServices`、`TestE2EHarnessUsesExternalServicesWhenProvided` |
| `client/test/host_link_test.dart` | 去掉 env skip；spawn isolate + presence 等待 + 授权等待；帧数断言改为「>0 且 ≤5」 |
| `client/test/session_e2e_test.dart` | 去掉 env skip，改由 harness 启服务 |
| `client/dart_test.yaml` | 声明 `e2e-desktop` 标签（Linux CI 排除；桌面矩阵归 ER-900） |
| `server/cmd/stack/main.go` | `ER_E2E_HARNESS=1` 时挂只读 `/debug/presence`（在线数），生产装配不挂载 |
| `server/internal/stack/rendezvous.go` | WS 升级失败日志（原来静默 return） |
| `core/crates/er-client/src/agent.rs` | `wait_authorized`；握手等待期间重发注册+握手；收到对端后补发自己的握手 |
| `core/crates/er-client/src/conductor.rs` | 主控连接流程接入授权等待与握手补发 |
| `core/crates/er-client/src/lib.rs` | HTTP 非 2xx 错误携带状态码/URL/原始响应体（诊断） |
| `client/lib/native.dart` | 修复 ErHostLink 构造参数被 lint 改写后变成 dynamic 的问题 |
| `scripts/gate.mjs` / `ci.yml` | Flutter 测试串行；Linux 排除桌面 E2E 标签 |

## 2. 关键根因与修复（否则 E2E 不可确定性通过）

1. **403 /v1/relay/ticket**：`connect/request` 成功只代表请求已登记，不代表团已同意；主控立刻取票 → SessionAuthWindowExceeded。
   修复：主控轮询 `/v1/connect/status` 到 authorized（10s deadline），再做握手。
2. **注册无 ACK**：中继注册是裸 UDP，丢一次后该端的所有数据都被 `plan failed: short frame` 丢弃。
   修复：等待握手期间每 300ms 重发注册 + 自己的握手包。
3. **先发的握手被丢**：A 在对端注册前发出握手并很快完成（因为收到了 B 的包），B 永远收不到 A 的。
   修复：收到对端握手后补发一次自己的握手（双方都做）。
4. **并行 flaky**：多个 suite 同时构建/启动服务抢 CPU/端口。
   修复：gate/CI 统一 `flutter test --concurrency=1`。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| harness 启停服务、端口释放、幂等 stop | `TestE2EHarnessStartsAndStopsServices` | PASS |
| 显式外部服务时不启动子进程 | `TestE2EHarnessUsesExternalServicesWhenProvided` | PASS |
| 无 env 的真实完整会话 | `设备码 + 验证码 的完整会话` | PASS（10 连跑 0 失败） |
| 错误验证码被拒绝 | `错误验证码被拒绝` | PASS |
| 真实登录/握手/加密/解码 | `完整会话：登录→建会话→握手→加密→解码` | PASS |
| 全量客户端 | `flutter test --concurrency=1` | 62 passed |

日志：`e2e-tests.log`、`flutter-full.log`、`gate.log`。

## 4. 稳定性验证

- 修复后 host E2E 连续 10 次全部通过（`fail=0`）。
- 全量 Flutter 串行连续 2 次 62 passed。
- gate 6 栈全绿。

## 5. 已知限制 / 归属

- `wait_authorized` 仍是有界轮询（50ms），最终应由信令事件驱动：ER-201。
- 中继注册仍无 ACK，当前靠重发；ACK/幂等属于 ER-501/ER-504。
- `/debug/presence` 仅在 `ER_E2E_HARNESS=1` 的内存 stack 挂载，生产 gateway/rendezvous 无此路由；后续 ER-902 需加负向测试。
- Linux CI 排除 `e2e-desktop`（无显示/FFI）；Windows/macOS 桌面矩阵归 ER-900。
- Flutter analyze 存量 111 info（本卡新增文件 0 warning）。

## 6. 回滚

- 回滚 harness/测试文件即可恢复旧 skip 行为（不得作为完成态）。
- 握手重发/补发与授权等待是产品可靠性修复，回滚会重新引入 403 与“双方干等”；如必须回滚，应保持 E2E 标记未完成。
- `/debug/presence` 可用环境变量关闭（默认不挂载）。
