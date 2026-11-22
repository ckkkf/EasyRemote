# ER-901 证据：性能基准
- 状态: 部分完成（30min/RSS 未跑）
- 脚本：bash scripts/bench/run.sh（SOAK_SECONDS=1800 可长跑）
- 原始：docs/impl/evidence/ER-901/raw/*.json|log；日志 bench.log
- 关键数据：50.9 fps、encode p95 24.13ms、decode p95 3.88ms、RTT 2.596ms。
- 未完成：30min soak、RSS、PRD 阈值判定、Windows。
