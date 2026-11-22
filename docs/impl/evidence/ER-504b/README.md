# ER-504b 证据：手动重连降级

- 状态: 完成（显式降级为手动重连）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| session_page.dart | 工具栏“重连”：close → attach → 重启轮询 |
| session_video_async_test.dart | TestManualReconnectReattaches |
| 卡/ADR 文案 | 不承诺自动重连/5s 阈值 |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 手动重连会再次 attach | TestManualReconnectReattaches | PASS |
| attach 失败不伪装已连接 | 同上 attached=false | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 命令与输出

    $ cd client && flutter test test/session_video_async_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 边界

- 没有自动重连；断网/Relay 重启需用户点“重连”或重进会话。
- 5s/10s 恢复阈值未实现，不得对外承诺。
- 重连不重新授权；依赖授权窗口（ER-205）；票据轮换仍生效。
- UI 只在会话页提供；无全局网络状态提示。

## 5. 回滚

- 回滚按钮会失去手动恢复入口，禁止；自动重连需单独实现并与本入口共存。
