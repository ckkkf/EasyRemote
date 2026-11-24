# ADM-API-03f 会话列表筛选（缺口卡）

> 状态：**未开始**（缺口卡；ADM-API-02f7 复核登记）
> 来源：`docs/spec/pages.md` A7（行 234-238）

## 缺什么

| 筛选 | 现状 |
|---|---|
| 状态（requested/authorized/active/closed） | 已有 |
| 时间范围（开始时间） | 缺（store.Page 有 From/To，但会话查询未用） |
| 账号 | 缺（需按 controller/target 设备的 account_id 过滤） |
| 设备 | 缺（详情页有按设备过滤，列表没有） |
| 链路类型（P2P/中继） | 缺（需按会话采样的主导链路过滤） |
| 是否异常结束 | 缺（需复用 `session.IsAbnormal`） |

## 为什么单独开卡

链路类型与异常结束都依赖"会话级聚合"（02f7 已提供摘要），但筛选必须下推到存储层（`ListSessions` 的 where 条件），不能前端过滤——属于契约 + 查询改动，单独一张卡。

## 建议拆分

- ADM-API-03f1：时间范围 + 账号/设备（sessions 关联 devices 的 account_id）。
- ADM-API-03f2：链路类型 + 异常结束（关联采样摘要 / 结束原因分类）。

## 验收提示

每项都要有"筛选在服务端生效"的证据（分页总数与列表一致、前端不做二次过滤）。

## 进度（ADM-API-03f1）

- 已完成：时间范围（started_at）、账号（按名下设备关联）、设备（controller/target）三项筛选，List/Count 共用 `sessionWhere`。
- 仍缺：链路类型筛选（需关联会话采样主导链路）、异常结束筛选（需复用 `session.IsAbnormal`）。
