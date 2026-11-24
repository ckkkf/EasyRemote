# ADM-UI-00b 页面矩阵复核（状态收敛）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-00 基线维护）
> 目的：把 A1-A14 的状态从"历史快照"收敛到当前真实实现，禁止虚标。

## 目标

1. 逐行核对矩阵状态与代码/接口/测试，修正过时描述；
2. 修正 A10/A11/A14 行的列错位（原型列误填代码路径）；
3. 为剩余缺口建**缺口卡**（只写卡，不写业务代码）；
4. `node scripts/check-admin-ui-matrix.mjs` 全绿。

## 复核结论（状态变化）

| 页面 | 旧状态 | 新状态 | 依据 |
|---|---|---|---|
| A5 设备列表 | partial | **implemented** | 11 列 + 在线筛选 + 强制下线/解绑/风险（ADM-API-04b/04c，E2E 26 checks） |
| A6 设备详情 | partial | **implemented** | 设备信息 11 项 + 按设备会话 + 近 30 天按日质量聚合（ADM-API-02d） |
| A2 仪表盘 | partial | partial（描述更新） | 质量组 P50/P95/中继占比已是真实采样（02e）；稳定性/趋势仍缺 |
| A3/A4 | partial | partial（描述更新） | 昵称/设备数/最近登录已真实；按账号会话/备注/导出仍缺 |
| A7/A8 | partial | partial（描述更新） | 会话四态/中断/每秒采样已真实；延迟/链路列与错误时间线仍缺 |
| A12 | partial | partial（描述更新） | 变更历史 + 单键回滚已真实；分组/作用域仍缺 |
| A13 | partial | partial（描述更新） | 动作/结果/操作人/对象/时间范围筛选已真实；CSV 导出仍缺 |
| A10/A11/A14 | blocked-by-api | **implemented** | 09b/09c/09d 页面 + 06a/06b/06c 接口 |

## 新建缺口卡

- `docs/impl/tasks/ADM-API-02f-stability-trends.md`（稳定性组、建连/P2P/帧率、趋势图、会话链路列）
- `docs/impl/tasks/ADM-UI-07b-kill-double-confirm.md`（A9 双人确认）
- `docs/impl/tasks/ADM-API-05c-config-scope-export.md`（配置分组/作用域 + 审计 CSV 导出）

## 必须通过的断言

- [ ] `node scripts/check-admin-ui-matrix.mjs`：20 行齐全、证据存在、缺口卡存在、代码引用带行号。
- [ ] 每条 `implemented` 都有对应 E2E/verification 证据（不是"看起来能点"）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/impl/evidence/ADM-UI-00/page-matrix.md`
- `docs/impl/tasks/ADM-API-02f-stability-trends.md`、`ADM-UI-07b-kill-double-confirm.md`、`ADM-API-05c-config-scope-export.md`
- `docs/impl/tasks/ADM-UI-00b-matrix-reconcile.md`、`docs/impl/evidence/ADM-UI-00b/**`

禁止：为了让矩阵好看而把 partial 改成 implemented；删除缺口说明。

## 验收命令

```bash
node scripts/check-admin-ui-matrix.mjs
bash scripts/gate.sh
```

## 完成标准

两条断言全绿；矩阵描述与代码一致；剩余缺口全部有卡。

## 回滚

`git checkout -- docs/impl/evidence/ADM-UI-00/page-matrix.md`；删除三张缺口卡与 `docs/impl/evidence/ADM-UI-00b/`。
