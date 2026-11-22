# ER-903c 证据：会话级 relay 重启恢复
- 状态: 完成（relay）
- 脚本：scripts/chaos/session_resume.sh
- 日志：docs/impl/evidence/ER-903c.log、docs/impl/evidence/ER-903c/session-resume.log
- 关键：before 2/2 → 重启 → resumed after 0 retries → after 2/2 → SESSION RESUME PASSED
- 未完成：PG/Redis 重启、磁盘满、时钟偏差、Flutter 自动 rejoin。
