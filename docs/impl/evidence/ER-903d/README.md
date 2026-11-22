# ER-903d 证据：PG/Redis 故障
- 状态: 部分完成（发现 healthz 非 readiness）
- 脚本：scripts/chaos/db_redis_restart.sh；日志：docs/impl/evidence/ER-903d.log、ER-903d/stack.log
- 结果：Redis kill→401，restart+relogin→200；PG kill→500，restart→200；healthz 全程 200。
- 未完成：healthz readiness、磁盘满、时钟偏差。
