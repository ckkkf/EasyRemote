# ER-711c 证据：最后块 ACK
- 状态: 部分完成（顺序修正；无 E2E）
- 变更：host_stream 最后块 finish+write 成功后才 ACK；失败记录 last_error。
- 未完成：真实 E2E、ACK 超时重传、暂停/续传。
