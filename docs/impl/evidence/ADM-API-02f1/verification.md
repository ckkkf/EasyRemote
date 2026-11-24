# ADM-API-02f1 验证记录（结束原因归一 + 异常中断率）

> 卡：`docs/impl/tasks/ADM-API-02f1-close-kinds.md`
> 时间：2026-11-23 14:41-14:50（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/session/closekind.go` | `ClassifyClose` / `IsAbnormal`：结束原因归一的唯一来源 |
| `closekind_test.go` | 13 组分类 + 异常判定 |
| `docs/spec/contract.yaml`、生成物 | Overview 增 `stability_available / closed_sessions / abnormal_closes / abnormal_close_rate` |
| `adminapi/adminapi.go`、`stability_test.go` | 按窗口统计结束会话与异常中断率 |
| `web-admin` 三文件 | 稳定性组显示真实异常中断率；崩溃率/重连仍「数据未提供」 |
| `scripts/admin-ui-e2e-dashboard.py` | 真实关闭 `network changed` 会话后断言 100% |

## 2. 口径

| 原因文本 | 分类 | 算异常 |
|---|---|---|
| 空 | unknown | 是 |
| `admin: ...` / `admin force-offline: ...` | admin_kill | 否 |
| 含 timeout / expired | timeout | 是 |
| 含 network / connection lost | network_lost | 是 |
| 含 offline / peer closed | peer_offline | 是 |
| 含 hangup / user close | user_hangup | 否 |
| 其它 | unknown | 是 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 13 组归类 | `TestClassifyCloseKinds` | PASS |
| 异常判定边界 | `TestIsAbnormalExcludesUserAndAdmin` | PASS |
| 窗口统计 + 50% | `TestOverviewStabilityMetrics` | PASS |
| 端到端 100% | `TestDashboardE2E` | PASS 19/19 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（57 个类型 / 57 条接口）
--- PASS: TestClassifyCloseKinds
--- PASS: TestIsAbnormalExcludesUserAndAdmin
--- PASS: TestOverviewStabilityMetrics
PASS abnormal close rate from real reasons -> 100%
PASS stability subtitle shows closed count
PASS crash/reconnect still unavailable
E2E OK (19 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 未完成项、限制与风险

- 崩溃率、重连成功率仍无数据源（需客户端上报）；建连成功率/P2P 命中率/帧率达成率仍未做；趋势图未做。
- 分类基于文本匹配：客户端如果传自由文本（例如中文"网络断了"），会落到 `unknown` 并计入异常——这是**有意的保守口径**（宁可多算异常），但会让异常率偏高；后续应让客户端传枚举。
- 窗口按 `EndedAt` 过滤；历史会话（EndedAt 为零值）不计入。
- A7 会话行的延迟/链路列仍缺（同属 02f 剩余部分）。

## 6. 回滚

`git checkout --` 契约/生成物/adminapi/前端/E2E；删除 `closekind.go`、`closekind_test.go`、`stability_test.go` 与 `docs/impl/evidence/ADM-API-02f1/`。
