# ER-711 证据：文件能力禁用
- 状态: 部分完成（disabled + ADR-0005）
- file_transfer debug/release 均不可用（capability 测试 PASS）。
- 网关文件页不再暴露，避免冒充点对点。
