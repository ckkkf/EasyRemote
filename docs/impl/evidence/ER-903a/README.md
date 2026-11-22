# ER-903a 证据：active janitor
- 状态: 完成（30s 清理窗口）
- 新增：session/janitor.go、janitor tests；stack_pg 启动 30s janitor。
- 测试：TestJanitorClosesActiveWhenPeerOffline、TestJanitorSkipsOnPresenceError PASS；gate PASS。
- 边界：崩溃后最多 30s 残留 active；无媒体死锁检测。
