# ER-908 · PostgreSQL 会话列表和存储契约验收

成熟度：beta。只恢复 PostgreSQL 对现行 Store 会话契约的实现，并让存储契约使用生产迁移；不会提升整体部署或桌面成熟度。

## 字段映射
- ADM-07 与 `SessionRow`（`docs/spec/contract.yaml:507–512、100–112`）：N/A，既有查询和 DTO 不变。
- GW-06 / GW-08（`docs/spec/contract.yaml:366–378`）：N/A，无请求、响应、状态机或迁移变更。

## 能力链
后台会话列表（A7）→ adminhttp → adminapi `ListSessions` → PostgreSQL `ListSessions`，同一查询也服务会话清理器。SELECT 已返回 12 列，旧循环仍仅解码 8 列。单条读取与列表共用一个 decoder，确保状态、版本、就绪标志和关闭分类完整。信令、媒体、OS、前端与指标采集不改。

仓库的 PG 测试仍加载旧版单迁移，导致用例在进业务前失败。测试改用 `stack.LoadMigration` 并在随机命名的隔离 schema 建表和清理，不 truncate 既有数据。共享测试的设备先创建有外键关联的账户；不放宽原有业务断言。使用本机临时 PostgreSQL 实例验证，不对现有用户数据库操作。

## 验收
- `TestPGContract/SessionLifecycle`：有结果的列表与按 ID 查询一致，包括版本、准备标志和关闭分类；Memory 运行同一用例。
- `TestPGContract/ExpiredRequestReplacement`：真实生产迁移和 PostgreSQL 下，新申请不会复用过期授权的原会话；旧会话迁移和 outbox 同时可读。
- 真库完整契约以及 Go race、严格 gate；回退仅本卡实现和测试，无接口或 schema 迁移。

## 结果（2026-11-24）

已复现旧读法的 12 列 / 8 个目标报错。修后 `TestPGContract/SessionLifecycle`、`TestMemoryContract/SessionLifecycle`、真实 PG 过期替换回归通过；PG 统计的独立问题由 ER-909 修复。PG 完整契约和严格闸门通过，PG 额外 tag 尚未纳入默认 gate。详见 [审查](../quality-audit-2026-11-24.md)。
