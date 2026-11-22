# ER-812 证据：中断结果
- 状态: 完成
- 变更：KillSessionResult；KillSession 先传播后审计；UI partial/timeout；文案修正。
- 测试：TestKillSessionReportsPartialPropagation PASS；kill 审计/契约测试 PASS；gate PASS。
- 未完成：真实 Bus 传播失败 E2E；超时 trace_id。
