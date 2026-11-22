# ER-901a 证据：30min soak
- 状态: 完成（macOS Software）
- 原始：media-soak.json、rss.log、rss-analysis.json、soak.log
- 结果：1800.01s / 102496 帧 / 56.9 fps；encode p95 15.81ms，decode p95 2.72ms。
- RSS：89,680KB → 74,752KB（-16.65%），斜率 -721 KB/min；峰值 90,896KB。无泄漏迹象。
- 未完成：Windows/硬件编码、弱网叠加。
