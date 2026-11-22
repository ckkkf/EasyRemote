# ER-903b 证据：relay 重启
- 状态: 部分完成
- 新测试：client/test/relay_restart_test.dart（真实进程 kill/重启，网关健康）
- 代码：E2eServices.restartRelay
- 未完成：会话级重连断言、PG/Redis 重启、磁盘满、时钟偏差。
- 命令：cd client && flutter test test/relay_restart_test.dart → All tests passed
