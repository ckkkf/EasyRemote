# ER-503a 证据：浏览器显式降级

- 状态: 完成（ADR + stub 测试；不实现浏览器 WebTransport）
- gate: PASS

## 1. 决策

- v1 浏览器不做 WebTransport/远程控制；只保留 HTTP+WS 控制面。
- 视频/被控入口 Web 端禁用并提示桌面端。
- webtransport_url 保留但不代表能力可用。

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| Web 视频 stub 不建立会话且提示桌面端 | TestBrowserVideoStubIsExplicitlyDisabled | PASS |
| Web 被控 stub 不支持 | TestBrowserHostServiceIsUnsupported | PASS |
| 发布能力门控隐藏 experimental | ER-000 capability 测试 | PASS |

## 3. 命令与输出

    $ cd client && flutter test test/web_transport_fallback_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 4. 边界

- 浏览器远控不可用是产品限制，需发布说明。
- wtransport 后续接入需重新评估证书/CORS/Origin/DoS。
- 未新增浏览器 WebTransport 依赖。

## 5. 回滚

- 回滚降级会让 UI 暗示浏览器可远控，禁止；必须同时提供真实数据面与测试。
