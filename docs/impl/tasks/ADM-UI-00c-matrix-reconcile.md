# ADM-UI-00c 矩阵复核（A3/A4/A9/A13 收口）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-00 基线维护）
> 目的：把上一轮之后新增的能力收敛进矩阵，重算真实剩余缺口。

## 状态变化

| 页面 | 旧 | 新 | 依据 |
|---|---|---|---|
| A3 用户列表 | partial | **implemented** | 03d/03e：解封、踢下线、注册时间范围、CSV 导出 |
| A4 用户详情 | partial | **implemented** | 03c：按账号会话、操作记录、备注 |
| A9 强制中断弹窗 | partial | **implemented** | 07b1/07b2：双人确认全链路 |
| A13 审计日志 | partial | **implemented** | 05a/05c2：服务端筛选 + CSV 导出 |
| A12 配置中心 | partial | partial | 只剩**版本作用域**（05c 剩余） |
| A2/A7/A8 | partial | partial | 剩余项都需要**客户端上报**（P2P 链路、目标帧率、崩溃/重连） |

统计：A1-A14 中 **implemented 10 / partial 4 / blocked 0**。

## 必须通过的断言

- [ ] `node scripts/check-admin-ui-matrix.mjs`：20 行齐全、证据存在、缺口卡存在、代码引用带行号。
- [ ] 状态统计脚本输出 `implemented: 10 / partial: 4 / blocked: 0`。
- [ ] 缺口卡更新进度（02f / 05c / 07b）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/impl/evidence/ADM-UI-00/page-matrix.md`
- `docs/impl/tasks/{ADM-API-02f-stability-trends,ADM-API-05c-config-scope-export,ADM-UI-07b-kill-double-confirm}.md`（追加进度）
- `docs/impl/tasks/ADM-UI-00c-matrix-reconcile.md`、`docs/impl/evidence/ADM-UI-00c/**`

禁止：把"需要客户端上报"的项标成 implemented；删除剩余缺口说明。

## 验收命令

```bash
node scripts/check-admin-ui-matrix.mjs
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout -- docs/impl/evidence/ADM-UI-00/page-matrix.md docs/impl/tasks/ADM-API-02f-stability-trends.md docs/impl/tasks/ADM-API-05c-config-scope-export.md docs/impl/tasks/ADM-UI-07b-kill-double-confirm.md`；删除 `docs/impl/evidence/ADM-UI-00c/`。
