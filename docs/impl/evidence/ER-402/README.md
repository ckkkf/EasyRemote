# ER-402 证据：FFI 所有权与 UI 阻塞

- 状态: 完成（画面/被控字符串释放 + attach 异步；gate PASS）
- 限制：HostService prepare/start 仍同步；长时 ASAN 未跑。

## 1. 变更

| 文件 | 内容 |
|---|---|
| session_video_io.dart | attach 改后台 Isolate.run；6 个字符串 finally 释放；poll/send/close 释放 session 指针；attachRunner 注入点 |
| session_video_stub.dart | attach 改 async；补 attachRunner |
| host_service_io.dart | prepare/ensurePassword/start/status/stop 输入字符串成对 free |
| session_page.dart | await attach，再启动 poll；unawaited 调用 |
| session_video_async_test.dart | attach pending 时 Timer 继续 tick |

## 2. 不变量

- 所有 native 输入字符串在 finally 释放；返回串由 er_free_string。
- attach 不占用 UI isolate；失败只记录 error。
- poll 失败不清空上一帧；close 释放 ui.Image。
- 主 isolate 在 attach 成功后重新打开库句柄，供 poll/send。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| attach 未完成时心跳仍运行 | TestAttachDoesNotBlockUiHeartbeat | PASS |
| E2E 会话/主机链路 | host_link / session_e2e / e2e_services | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 4. 命令与输出

    $ cd client && flutter test test/session_video_async_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- HostService.prepare/start 仍同步 FFI，网络阻塞会卡 UI；下一步改为 isolate/worker（需 host 状态回传协议）。
- native.dart 的 ErHostLink.serve 已在测试中走 Isolate；生产 self-test 也需检查。
- 没有 ASAN/长时间资源增长测试；需要 macOS/Linux 构建与压力脚本（ER-900）。
- FFI 会话句柄跨 isolate 依赖 Rust 进程级 registry；若未来改为每 isolate 独立，需要重新设计。

## 6. 回滚

- 回滚 attach 异步会重新阻塞 UI，禁止。
- 回滚字符串释放会恢复泄漏，禁止。
