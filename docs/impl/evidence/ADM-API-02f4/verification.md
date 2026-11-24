# ADM-API-02f4 验证记录（链路上报 / P2P 命中率 / 帧率达成率）

> 卡：`docs/impl/tasks/ADM-API-02f4-link-report.md`
> 时间：2026-11-23 17:56-18:05（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/013_quality_link.sql` | 采样表加 `link` / `target_fps` |
| `store/{store,memory,pg}.go` | `QualitySample.Link/TargetFps`、`QualityLinkStats` |
| `docs/spec/contract.yaml`、生成物 | `QualitySample.link/target_fps`、Overview 四个新字段 |
| `gateway/handler.go` | 入站校验：link 只收 p2p/relay，target_fps 限 0-240 |
| `adminapi/adminapi.go`、`link_stats_test.go` | P2P 命中率 / 帧率达成率 |
| `web-admin` 三文件 | 两个指标改为真值（未上报仍「数据未提供」） |
| `scripts/admin-ui-e2e-dashboard.py` | 24 条 E2E |

## 2. 口径

| 指标 | 分子 | 分母 | 说明 |
|---|---|---|---|
| P2P 命中率 | link=p2p | p2p+relay | 只统计上报过 link 的采样 |
| 帧率达成率 | fps ≥ target_fps | target_fps>0 | 只统计上报过 target_fps 的采样 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约一致 | `gen-contract --check` | PASS（68 类型 / 68 接口） |
| 无上报 / 50% / 67% | `TestOverviewLinkAndFpsRates` | PASS |
| 端到端 33% / 33% | `TestDashboardE2E` | PASS 24/24 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS（未变） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（68 个类型 / 68 个接口）
--- PASS: TestOverviewLinkAndFpsRates
PASS p2p hit rate from reported link -> 33%
PASS fps target rate from reported target -> 33%
E2E OK (24 checks)
BASELINE COMPARE OK（6 屏 DOM 判据一致）
GATE: PASS
```

## 5. 过程中修掉的问题

- 生成物字段名是 `P2pHitRate`（Go initialism 规则），我按 `P2PHitRate` 写导致编译失败；按生成物命名修正。
- 首次基线比对指向 `ADM-UI-10c/screens/manifest.json`（实际落在 `logs/screens` 下），改用 `ADM-API-03e` 的 manifest 后 PASS。

## 6. 未完成项、限制与风险

- **崩溃率 / 重连成功率仍缺**：需要客户端崩溃事件与重连结果上报（本卡未做）。
- `link` 只接受 p2p/relay；其它值静默按"未上报"处理（有意的 fail-safe，避免脏值污染命中率）。
- 采样表新增两列后无回填：历史采样 link 为空，因此历史窗口的 P2P 命中率不可用（显示「数据未提供」）。
- 客户端尚未真正上报 link/target_fps；当前 E2E 用设备凭证模拟上报，真实链路数据要等客户端接入。

## 7. 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 013 迁移、`link_stats_test.go` 与 `docs/impl/evidence/ADM-API-02f4/`。
