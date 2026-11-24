# 里程碑 2f：GW-25 路径纠偏（ER-905）

## 监督反查结论（已核实）

- `docs/spec/contract.yaml` GW-25：`method: POST`，`path: /v1/sessions/close`，`response: SessionStateResult`
- `server/internal/gateway/handler.go:523`：`mux.HandleFunc("/v1/sessions/close", ...)`
- `client/lib/contract.dart:1685`（生成物）：`EndpointSpec(id: 'GW-25', method: 'POST', path: '/v1/sessions/close', ...)`

**之前实现与测试写的是 `/v1/connect/close` —— 服务端没有该路由，请求必然 404，
主控「断开连接」永远失败，四个现象里的 2/3 会因此复现。**

## 改动

| 文件 | 改动 |
|---|---|
| `client/lib/api.dart` | `closeSession` 路径改为 `/v1/sessions/close`；注释点明来源是 GW-25 契约 |
| `client/test/session_exit_routes_test.dart` | mock 与断言不再手写路径：`closePath = endpoints.firstWhere((e) => e.id == 'GW-25').path`；`closedCalled()` 用**精确相等** `c == 'POST ' + closePath` |
| `client/test/close_path_contract_test.dart`（新增） | `TestCloseSessionUsesContractGw25Path`：断言 GW-25 契约路径是 `/v1/sessions/close`、`closeSession` 请求该路径、且**不得**出现 `/v1/connect/close` |
| `docs/impl/evidence/ER-905/{diagnosis,07,09}.md` | 错误的 `/v1/connect/close` 全部改为 `/v1/sessions/close` |

## 定向测试（先独立跑绿，再进 gate）

```
$ flutter test test/close_path_contract_test.dart
TestCloseSessionUsesContractGw25Path   PASS

$ flutter test test/close_path_contract_test.dart test/session_exit_routes_test.dart test/session_close_coordinator_test.dart
00:00 +11: All tests passed!

$ cd server && go test ./internal/gateway -run 'SessionClose|ClosePropagates|ConsentRejected' -v
--- PASS: TestClosePropagatesToBothPeers
--- PASS: TestConsentRejectedReturnsCompletedClosedState
ok  github.com/easyremote/server/internal/gateway  1.111s
```

## 守护机制

测试不再手写路径：唯一真相是生成的 `client/lib/contract.dart` 里 GW-25 的 `path`。
任何人把实现改回 `/v1/connect/close`，`TestCloseSessionUsesContractGw25Path` 会同时失败于
"未调用契约路径" 与 "出现了不存在路径" 两条断言。

## Gate

- 完整输出：`10-gate-after-path-fix.txt`（`EXIT=0`，`GATE: PASS`）
