# ADM-UI-00b 验证记录（矩阵复核）

> 卡：`docs/impl/tasks/ADM-UI-00b-matrix-reconcile.md`
> 时间：2026-11-23 14:36-14:44（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/impl/evidence/ADM-UI-00/page-matrix.md` | 14 行状态与描述按当前实现重写；修正 A10/A11/A14 列错位；代码引用补行号 |
| `docs/impl/tasks/ADM-API-02f-stability-trends.md` | 新缺口卡：稳定性组/建连埋点/趋势图/会话链路列 |
| `docs/impl/tasks/ADM-UI-07b-kill-double-confirm.md` | 新缺口卡：A9 双人确认 |
| `docs/impl/tasks/ADM-API-05c-config-scope-export.md` | 新缺口卡：配置分组/作用域 + 审计 CSV 导出 |

## 2. 状态变化（只升不虚标）

| 页面 | 旧 | 新 | 证据 |
|---|---|---|---|
| A5 设备列表 | partial | implemented | ADM-API-04b/04c（E2E 26 checks） |
| A6 设备详情 | partial | implemented | ADM-API-02d（按日质量聚合） |
| A10 风控中心 | blocked-by-api | implemented | ADM-UI-09c（E2E 14 checks） |
| A11 版本管理 | blocked-by-api | implemented | ADM-UI-09d（E2E 14 checks） |
| A14 管理员与角色 | blocked-by-api | implemented | ADM-UI-09b（E2E 13 checks） |
| A2/A3/A4/A7/A8/A9/A12/A13 | partial | partial（描述更新） | 见各行证据列 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 20 行齐全、A1-A14 + s12-s17 覆盖 | `TestAdminUiMatrixRowsComplete` | PASS |
| 证据路径与 6 张截图存在 | `TestAdminUiMatrixEvidenceExists` | PASS |
| 缺口卡存在、代码引用带行号 | 同上（checker 内部校验） | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
PASS TestAdminUiMatrixRowsComplete: 20 行，A1-A14 与 s12-s17 全覆盖
PASS TestAdminUiMatrixEvidenceExists: 证据路径与 6 张 1360x860 截图均存在
GATE: PASS
```

## 5. 复核中发现的问题

- A10/A11/A14 三行的列被写错（"原型"列填了代码路径），checker 之前没发现，因为只校验状态与证据存在；本轮已修正列位。
- A5/A6 行仍写着"缺接口"，与 04b/04c/02d 的实际实现矛盾——属于**过时描述**，会让后续判断误以为还阻塞。
- 三处 partial 缺口卡此前只在描述里写了 `ADM-API-02` 这种笼统编号，checker 无法验证；本轮改为具体缺口卡文件。

## 6. 未完成项（Goal 仍 active）

- A2：稳定性组/建连成功率/P2P/帧率达成率/趋势图 → `ADM-API-02f`
- A3/A4：按账号会话、操作记录、备注、CSV 导出 → `ADM-API-03`（既有卡）
- A7/A8：延迟/链路/结束原因列、错误时间线 → `ADM-API-02f`
- A9：双人确认 → `ADM-UI-07b`
- A12/A13：配置分组/作用域、审计 CSV 导出 → `ADM-API-05c`

## 7. 回滚

`git checkout -- docs/impl/evidence/ADM-UI-00/page-matrix.md`；删除三张缺口卡与 `docs/impl/evidence/ADM-UI-00b/`。
