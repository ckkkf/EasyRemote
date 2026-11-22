# ER-603 证据：真实质量指标

- 状态: 部分完成（UI 数据面指标；后台未接）
- gate: PASS

## 1. 变更

| 层 | 内容 |
|---|---|
| ctrl_session | 1 秒窗口 bitrate/loss；decode_fps/decode_ms/dropped |
| er-ffi | er_ctrl_info 增加 bitrate_kbps/loss_pct |
| SessionVideo | 暴露 bitrateKbps/lossPct/presentFps/nativeFps/decodeMs |
| 质量抽屉 | 真实 FPS/码率/丢包/解码耗时；数据面优先，否则网关 ping |

## 2. 验收断言 → 证据

| 断言 | 证据 | 结果 |
|---|---|---|
| FPS/码率来自数据面 | ctrl_session 统计 + FFI 字段 | 已接 |
| 丢包优先数据面 | loss_pct 来自 ReassemblyStats | 已接 |
| 无“未接入（ER-603）”占位 | 质量抽屉文案/字段 | 已改 |
| 既有 E2E/闸门 | gate | PASS |

## 3. 未完成

- 后台质量统计（admin/质量报表）未读取同一数据源。
- 端到端媒体延迟（需要时钟同步/RTT 语义）未提供；当前只有解码耗时。
- 30 分钟稳定性/累积延迟未验证。
- UI 无自动化断言（页面依赖真实 SessionVideo 单例）。

## 4. 命令

    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS
