# ADM-API-03f2 会话筛选：链路类型 + 异常结束（缺口卡）

> 状态：**未开始**（缺口卡；ADM-API-03f1 之后登记）
> 来源：`docs/spec/pages.md` A7（行 234-238）

## 缺什么

| 筛选 | 依赖 |
|---|---|
| 链路类型（P2P / 中继 / 混合） | 会话采样里的 p2p/relay 计数（02f7 已有 `SessionSummaries`） |
| 是否异常结束 | `session.IsAbnormal(reason)`（02f1 已有） |

## 为什么单独开卡

两个筛选都要把"聚合结果"变成 SQL 条件：

- 链路类型需要 `session_quality_samples` 的 `count(*) FILTER (WHERE link='p2p')` 与 relay 比较；
- 异常结束需要把 `session.ClassifyClose` 的文本规则落成 SQL 表达式（或落库成 `close_kind` 列）。

后者更稳的做法是**新增 `sessions.close_kind` 列**（写会话关闭时落库），避免在 SQL 里重复一份分类规则。

## 建议拆分

- ADM-API-03f2a：`sessions.close_kind` 列 + 关闭时落库 + 迁移回填。
- ADM-API-03f2b：列表按 `close_kind` 与链路类型过滤（List/Count 共用 where）。

## 验收提示

- 异常结束筛选的结果必须与"结束原因列"展示的分类一致（同一套规则）；
- 链路类型筛选要与"链路列"展示的主导链路一致（同一套聚合）。
