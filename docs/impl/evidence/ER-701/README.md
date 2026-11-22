# ER-701 证据：隐私屏禁用与移除错误路径

- 状态: 完成（disabled + ADR）
- gate: PASS

## 1. 变更

- ADR-0004：隐私屏 v1 disabled；无本机显示隐藏实现。
- 删除 er-capture/privacy（涂黑采集帧）及示例。
- 删除 FFI er_set_privacy/er_privacy_scrub/er_last_frame_uniform。
- 客户端移除隐私 FFI 调用与测试。
- 能力台账保持 privacy_screen=disabled。

## 2. 验收

| 断言 | 证据 | 结果 |
|---|---|---|
| 不再有涂黑采集帧路径 | 代码扫描/文件删除 | PASS |
| 能力不可用 | capability 测试 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 边界

- 隐私屏仍不可用；需平台级本机显示隐藏 + 真机验证后才能启用。
- 未实现“本机黑屏但采集继续”。

## 4. 回滚

- 不得恢复涂黑路径冒充隐私屏。
