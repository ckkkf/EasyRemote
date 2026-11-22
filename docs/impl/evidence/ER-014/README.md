# ER-014 证据：两端完整保留错误信封

- 状态: 完成（bash scripts/gate.sh → GATE: PASS）

## 1. 交付

| 文件 | 目的 |
|---|---|
| client/lib/api.dart | ApiException 增加 name/retryable/traceId/statusCode；非对象 data 不再静默成 {}；超时/非 JSON 标记 retryable |
| client/lib/error_view.dart | 统一错误视图：message + code/name + trace_id 复制 + retryable 驱动的重试 |
| client/lib/pages/login_page.dart、connect_page.dart | 接入 ApiErrorView |
| client/test/api_error_test.dart、error_view_test.dart | 3+2 条测试 |
| web-admin/src/api.ts | toApiError：非信封响应也保留 status/retryable |
| web-admin/src/errors.ts | errorMeta：retryable + 是否有重试回调 → canRetry |
| web-admin/src/App.tsx | ErrorBanner 展示并复制 trace_id；可重试时显示“重试” |
| web-admin/tests/error-envelope.test.ts | 3 条 vitest |

## 2. 字段映射

| 信封字段 | Flutter | React |
|---|---|---|
| code | statusCode + displayName | ApiError.code |
| name | ApiException.name | ApiError.name |
| message | ApiException.message | ApiError.message |
| retryable | ApiException.retryable | ApiError.retryable → canRetry |
| trace_id | ApiException.traceId（可复制） | ApiError.traceId（可复制） |

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| Flutter 错误对象保留全部字段 | TestApiExceptionKeepsEnvelopeFields | PASS |
| 非 Map 成功 data 不被静默丢弃 | TestNonMapSuccessDataIsNotDropped | PASS |
| 正常 Map data 仍可解析 | TestMapSuccessDataStillParses | PASS |
| trace 可复制、retryable 决定重试按钮 | TestErrorViewShowsTraceAndRetryOnlyWhenRetryable | PASS |
| 不可重试错误不显示重试/复制 | 不可重试错误不显示重试按钮 | PASS |
| React 保留信封字段 | TestWebApiErrorKeepsEnvelopeFields | PASS |
| 非信封错误结构化 + retryable | TestWebNonEnvelopeErrorIsStructured | PASS |
| errorMeta 驱动复制/重试 | errorMeta drives retry button and trace copy | PASS |

## 4. 命令与输出

    $ cd client && flutter test test/api_error_test.dart test/error_view_test.dart
    5 tests passed
    $ cd web-admin && npm test -- --run
    6 tests passed（含本卡 3 条）
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界 / 回滚

- Flutter 错误码分支仍使用 name 字符串（兼容既有调用），numeric code 保留展示/诊断。
- 非对象 data 目前直接判为契约违规；若未来某端点返回标量，必须先改契约并扩展解码。
- Web ErrorBanner 的“重试”只在调用方提供 onRetry 时出现；其余页面仍可关闭后手动重试。
- 回滚会重新丢失 trace_id/retryable，禁止作为长期状态。
