# ER-600 证据：媒体能力矩阵

- 状态: 完成（软件降档；硬件矩阵待实机）
- gate: PASS

## 1. 实测

    cargo run --release -p er-codec --example media_capability
    {"backend":"Software","width":1920,"height":1080,"requested_fps":60,
     "encoded":120,"elapsed_ms":8078.9,"fps":14.9,"mbps":0.2}

结论：本机软件编码 1080p 只有 ~15fps，不能宣称 1080p60。

## 2. 降档规则

| 能力 | 分辨率 | fps | 码率 |
|---|---|---|---|
| 软件 | ≤1920x1080 | 1080p=15、720p=30、更低=60 | ≤8000 |
| 硬件（NVENC 可用） | ≤3840x2160 | 60 | ≤50000 |

请求 4K/60 在软件下降为 1080p/15，reason 记录在 last_error/日志。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 软件 1080p60 降到 ≤15 | software_1080p60_downgrades_fps | PASS |
| 720p60 降到 ≤30 | software_720p_downgrades_less | PASS |
| 4K 夹到 1080p/码率 | software_4k_is_clamped | PASS |
| 硬件保持 1080p60 | hardware_keeps_1080p60 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 4. 边界

- 硬件矩阵仅 Windows NVENC 探测；macOS VideoToolbox/Linux 未实现，按软件降档。
- 软件 720p/低分辨率 fps 是按像素面积估算，未分别实测。
- host_stream/pair 已用解析配置；控制器 12fps 限速与每帧 RGBA 拷贝仍是 ER-601。
- 未做 30 分钟稳定性/热降频；ER-602/903。

## 5. 回滚

- 回滚降档会重新宣称 1080p60，禁止。
- 降档必须保持 low-latency（bframes=0，gop<=fps）。
