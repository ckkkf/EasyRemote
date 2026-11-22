# ER-711e 证据：ACK 重发
- 状态: 完成（固定间隔重发/幂等）
- 测试：retransmit_until_ack、duplicate_chunk_is_idempotent PASS；文件 E2E All tests passed (46s)
- 日志：docs/impl/evidence/ER-711e.log
- 未完成：退避/上限、真实丢 ACK E2E、跨会话续传。
