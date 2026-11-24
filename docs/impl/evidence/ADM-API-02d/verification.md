# ADM-API-02d 验证记录（设备按日质量 + 迁移目录修复）

> 卡：`docs/impl/tasks/ADM-API-02d-device-quality.md`
> 时间：2026-11-23 13:04-13:14（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/003-005*.sql` | **从仓库根目录移入 embed 目录**（此前根本没被打包，真实 PG 上不生效） |
| `server/internal/stack/migration_test.go` | `TestLoadMigrationIncludesAllFiles`：迁移必须包含 accounts/quality/nickname/device_name |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | ADM-18 + `DeviceQualityDay`/`DeviceQualityResult` |
| `store/{store,memory,pg}.go`、`storetest` | `AggregateDeviceQuality`（同日平均/跨天分组/窗口过滤） |
| `adminapi/adminapi.go`、`device_quality_test.go` | `DeviceQuality`（available 语义 + 权限） |
| `adminhttp/handler.go` | 新路由 |
| `web-admin` | A6 真实按日聚合表 + 7/30 天切换 |
| `scripts/admin-ui-e2e-devices.py` | 真实上报两天采样 → 断言聚合行 |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-18 `GET /admin/v1/devices/quality?device_id=&days=30` | — | `{device_id, available, days:[{date, avg_latency_ms, avg_loss_permille, relay_percent, samples}]}` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（46 端点） |
| 迁移文件被打包 | `TestLoadMigrationIncludesAllFiles` | PASS |
| 聚合正确性 | `TestMemoryContract/AggregateDeviceQuality` | PASS |
| available 语义 + 权限 | `TestDeviceQualityAggregates` | PASS |
| 端到端（真实上报 → 聚合渲染） | `TestDevicesE2E` | PASS 21/21 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 路由 method/path/auth 与契约一致（46 条端点）
--- PASS: TestLoadMigrationIncludesAllFiles
--- PASS: TestMemoryContract/AggregateDeviceQuality
--- PASS: TestDeviceQualityAggregates
PASS device quality aggregated by day -> 2
   2026-11-22  30  2  0%   1
   2026-11-23  40  5  100% 1
E2E OK (21 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 本轮发现并修复的严重缺陷

**迁移文件放错目录**：ADM-API-03b/04a/02c 期间新增的 003/004/005 被写到仓库根 `migrations/`，而 `go:embed` 读的是 `server/internal/stack/migrations/`。后果是：内存 store 测试全绿，但真实 PG 上 `nickname`/`last_login_at`/`device_name` 等列和 `session_quality_samples` 表**从未创建**。本轮已移动文件并加 `TestLoadMigrationIncludesAllFiles` 守住。

## 6. 未完成项、限制与风险

- 仪表盘的质量/稳定性指标组、24h/7d/30d 趋势图仍缺（需要跨设备聚合与图表库）。
- 采样无 TTL/清理策略；按日聚合是查询时计算，量大后需要物化视图/定时任务。
- 客户端仍未真正上报采样；真实设备会显示「没有质量采样」。
- `server/migrations/`（旧目录，被 `pg_contract_test` 引用）与 embed 目录并存，属历史重复，需单独收敛。

## 7. 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E；删除 `migration_test.go`、`device_quality_test.go` 与 `docs/impl/evidence/ADM-API-02d/`。
