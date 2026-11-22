# ER-700 证据：本地常驻控制条

- 状态: 完成（本机控制条 + 关闭链路）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| local_control_bar.dart | 常驻条/断开按钮/无关闭入口 |
| device_list_page.dart | hasSession 置顶显示；断开调用 closeSession |
| host_service_io/stub | closeSession FFI/降级 |
| er-client | Gateway.close_session/TargetAgent.close_session |
| er-ffi | er_host_close_session |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 控制条显示“仅本机可断开” | TestLocalControlBar... | PASS |
| 无隐藏/关闭入口 | 同上（无“关闭”tooltip） | PASS |
| 一键断开回调 | 同上 | PASS |
| 服务端 close/传播 | 复用 ER-202 测试 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- 无“远端无法隐藏”的端到端测试（需要真实远端 UI/输入）。
- 全屏/纹理路径下控制条布局未实机验证（Xcode 缺失）。
- 断开失败时 UI 只恢复 idle，未显示具体错误。

## 4. 下一步

ER-701/702：隐私屏/防休眠接入主路径（语义必须是本机物理显示不可见、远端采集继续）。
