# ADR-0002: 浏览器传输降级（ER-503a）

- 状态: 接受（v1 不做浏览器 WebTransport；显式降级）
- 日期: 2026-11-22
- 关联: ADR-0001、ER-503、ER-902

## 背景

ER-503 完成了原生 QUIC 的通道映射（控制 uni / 数据 bidi / 视频 datagram），但浏览器 WebTransport 适配（wtransport/h3）尚未实现。Flutter Web 端当前：

- session_video_stub：不解码视频，attached=false；
- HostService stub：不支持被控采集/注入；
- 远端控制能力在台账中被能力门控为 experimental（发布隐藏）。

## 决策

v1 不宣称浏览器 WebTransport/远程控制：

1. 浏览器只保留 HTTP + 信令（WebSocket）控制面；不建立 WebTransport 数据面。
2. 视频/输入入口在 Web 端显式禁用（stub 报错），不显示“可操作的空壳”。
3. 能力台账 remote_control 在发布构建隐藏；Web 开发构建如需调试，仅能使用已有 HTTP/WS 流程，不伪装数据面。
4. WebTransport 适配作为后续独立卡：wtransport 优先；若不可行，保持 WS/Relay 降级。

## 后果

- 浏览器暂时不能远控；原生桌面能力不受影响。
- 需在 UI/文案中明确“需要桌面端”，禁止承诺浏览器实时画面。
- 后端 webtransport_url 字段保留但浏览器不消费；不得据此判定能力可用。

## 验证

- Web stub 行为测试：attach 不建立会话、error 明确提示桌面端；HostService stub 不支持。
- 发布能力门控隐藏 experimental（ER-000 已有测试）。
- 不新增浏览器 WebTransport 依赖/声明。

## 风险

- 浏览器路线延后可能影响产品承诺；需在发布说明中明确。
- 未来接入 wtransport 时需重新评估证书、CORS、Origin、数据通道与 DoS 面。
