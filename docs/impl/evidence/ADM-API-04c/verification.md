# ADM-API-04c 验证记录（设备风险标记）

> 卡：`docs/impl/tasks/ADM-API-04c-device-risk.md`
> 时间：2026-11-23 13:26-13:36（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/006_device_risk.sql` | `risk_flag` / `risk_reason` 两列（幂等） |
| `store/{store,memory,pg}.go`、`storetest` | `SetDeviceRisk`（解除时清空原因） |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `DeviceRow.risk_flag/risk_reason` + ADM-19/20 |
| `adminapi/adminapi.go` | `FlagDeviceRisk`/`ClearDeviceRisk`（权限 + 原因 + 审计），权限表新增两项 |
| `adminhttp/handler.go` | 两条路由 |
| `web-admin` | 列表「风险」列 + 标记/解除按钮与原因对话框 |
| `scripts/admin-ui-e2e-devices.py` | 26 条 E2E（含风险标记/解除 + 审计） |
| `web-admin/tests/admin-scope.test.ts` | 白名单同步 |

## 2. 字段映射

| 字段/端点 | 说明 |
|---|---|
| `DeviceRow.risk_flag` / `risk_reason` | 人工标记；解除时原因为空 |
| ADM-19 `POST /admin/v1/devices/flag-risk` | `DeviceActionRequest` → `OkResult`；权限 `device.flag_risk`（super/risk/ops） |
| ADM-20 `POST /admin/v1/devices/clear-risk` | 同上；权限 `device.clear_risk` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（48 端点） |
| 标记/解除/清空原因/未知设备 | `TestMemoryContract/DeviceRisk` | PASS |
| 权限/原因/审计动作 | `TestDeviceRiskFlagging` | PASS |
| 端到端标记与解除 | `TestDevicesE2E` | PASS 26/26 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（48 个类型 / 48 条接口）
✓ 路由 method/path/auth 与契约一致（48 条端点）
--- PASS: TestMemoryContract/DeviceRisk
--- PASS: TestDeviceRiskFlagging
PASS risk badge shown
PASS risk cleared
E2E OK (26 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 未完成项、限制与风险

- 风险标记只影响列表/详情展示与审计，**不会自动阻断连接**；自动处置（黑名单联动）属 A10 风控中心，需要独立设计。
- 解除风险要求填写原因（防止悄悄解除），审计同时保留 flag 与 clear 两条记录。
- 设备列表新增「风险」列，A5 未纳入六屏像素基线，列宽策略由响应式审计与 E2E 列断言覆盖。

## 6. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 006 迁移与 `docs/impl/evidence/ADM-API-04c/`。
