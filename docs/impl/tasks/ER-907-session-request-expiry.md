# ER-907 · 建连重试不得复用过期会话

成熟度：beta（不提升远控登记等级）。本卡只修服务端同一设备对的申请幂等与过期，不改公开接口、数据库 schema 或产品文档。

## 字段映射
- GW-06 的请求及 `session_id` / `state` / `trusted`（contract.yaml:366–370、748–753）：N/A，字段/语义不变。
- GW-08 的 `session_id` / `state` / `reason`（375–378、761–766）：N/A，沿用 `request_expired`、`auth_window_exceeded`。
- 时限来自 PRD §5.1.2–5.1.3，协议 §3；保持既有边界语义，不借本卡修改超时。

## 调用链与设计
客户端发起 → gateway GW-06 → session.Service.Request → Store.CreateSessionExclusive（Memory / PG） → 状态机 CAS / outbox → gateway → 信令 → 被控端。OS 采集、UI 与传输 N/A，无改动。诊断与验收使用服务已有会话事件（状态、原因和 trace）。

旧实现先查全局按开始时间取前 500 条，无关历史达到截断上限后，设备对的过期会话可能漏过，再被原子插入的防重分支原样返回。改用 Store 原子返回值判定当前设备对，不再扫描全局会话。回收时按 CAS 重读状态并重新判过期，不得把已被并发操作推进为 active 的会话关闭；关失败必须上抛，不得返回已过期会话。保留 active 不作为申请过期回收对象。

## 验收
- `TestRequestDoesNotReuseExpiredSessionBeyondHistoryLimit`：501 条无关记录下分别验证 requested、authorized 过期、旧状态收敛、关闭事件和新状态。
- `TestConcurrentRetryOfExpiredSessionCreatesOneReplacement`：并发竞争只产生一个新会话。
- `TestRequestExpiryDoesNotCloseConcurrentlyActivatedSession`：不依据过期旧快照关闭已变更的会话。
- `TestRequestDoesNotReturnExpiredSessionWhenCloseWriteFails`：持久化失败不会冒充成功。
- 跑 Go race、strict gate；如果无法验证真实 PostgreSQL 记录限制。回退仅此卡的 session 包改动，无数据迁移；若必须回退，注意旧历史截断问题会重现。

## 结果（2026-11-24）

四个新增回归均通过，真实 PostgreSQL 的 `TestPGContract/ExpiredRequestReplacement` 通过，Go race 和严格闸门通过。范围不含后台主动到期扫描、票据签发及生产 outbox 投递。详见 [审查与命令](../quality-audit-2026-11-24.md#验收命令与实测结果)。
