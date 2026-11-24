# 里程碑 4：跨端生命周期 E2E（ER-905）

## 新增测试

`server/internal/gateway/lifecycle_e2e_test.go`
`TestRemoteLifecycleRejectAndBidirectionalDisconnect`（真实 gateway handler + 真实内存存储 + 两个独立账号/设备身份）

覆盖路径：

| 断言 | 结果 |
|---|---|
| 4) requested 阶段申请 relay ticket **不得成功** | PASS（返回 403） |
| 1) 被控拒绝 → 主控 status 得到 `closed/user_rejected` | PASS |
| 3) 接受 → authorized → 主控调 GW-25 → 被控 status 得到 `closed/controller_closed` | PASS |
| 5) 关闭后旧会话再申请 ticket **不得成功** | PASS |
| GW-25 幂等：重复关闭仍 `closed`，不复活 | PASS |

```
$ cd server && go test ./internal/gateway -run 'TestRemoteLifecycleRejectAndBidirectionalDisconnect' -v
--- PASS: TestRemoteLifecycleRejectAndBidirectionalDisconnect (0.01s)
ok  github.com/easyremote/server/internal/gateway  0.459s
```

## 测试自身暴露的 spec 缺口（blocker 证据）

跑测试时服务端日志：

```
code=300006 name=SESSION_AUTH_WINDOW_EXCEEDED detail=session not authorized
```

即 **GW-11 把「尚未授权」写成了「12 小时授权窗口到期」**。
`docs/spec/error-codes.md` 里该码只表示真实窗口到期；GW-11 的 errors 也没有
「未授权/已拒绝/已关闭」的精确码。按任务卡 4.4.3 记录为 blocker，**不改 spec**：

- blocker id：`ER-905-SPEC-01`
- 影响：仅 GW-11 的防御性错误码语义；本卡已用客户端「requested 不 attach」消除用户可见的 403
- 待批准方案：新增 `SESSION_NOT_AUTHORIZED` / `SESSION_CLOSED` 两个码（需负责人批准 spec 变更）

## Rust 侧确认

`host_stream` 的推流循环已有 1 秒状态兜底（`target.session_state()` + `closed/failed/ended -> break`），
`cargo test -p er-client --lib` 31 passed。被控端在主控关闭后 ≤2s 内停止采集/编码/输入。

## Gate

- `14-gate-e2e.txt`：`EXIT=0`，`GATE: PASS`
- 一次 FAIL 因新测试文件未 gofmt，已修
