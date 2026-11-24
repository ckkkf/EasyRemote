# ADM-API-04c 设备风险标记 + 解除

> 来源：`docs/impl/tasks/ADM-API-04-device-contract.md`（操作：标记风险）
> 需求：`docs/spec/pages.md` A5（行 227）、PRD §6.3.3（行 598）
> 前置：ADM-API-04a/04b（设备元信息与动作框架）

## 目标

给设备加人工风险标记（标记/解除都要求原因、都写审计），管理端列表显示风险徽标；**不自动阻断连接**（自动处置属 A10 风控中心）。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-19 `POST /admin/v1/devices/flag-risk` | `{device_id, reason}` | `OkResult` | `device.flag_risk`（super/risk/ops） |
| ADM-20 `POST /admin/v1/devices/clear-risk` | 同上 | `OkResult` | `device.clear_risk` |

`DeviceRow` 增 `risk_flag: bool`、`risk_reason: string`。

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：48 条端点一致。
- [ ] `TestMemoryContract/DeviceRisk`：标记写入、解除清空原因、未知设备 `ErrNotFound`。
- [ ] `TestDeviceRiskFlagging`：risk 角色可标记/解除；缺原因 `ADMIN_REASON_REQUIRED`；readonly `ADMIN_FORBIDDEN`；审计含 flag/clear。
- [ ] `TestDevicesE2E`：列表 11 列（含风险）、标记后出现徽标且写审计、解除后徽标消失且写审计。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/006_device_risk.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`device_risk_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`tests/admin-scope.test.ts`
- `scripts/admin-ui-e2e-devices.py`
- `docs/impl/tasks/ADM-API-04c-device-risk.md`、`docs/impl/evidence/ADM-API-04c/**`

禁止：手改生成物；标记风险却阻断连接（超出本卡范围）；解除风险不写审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ -run 'DeviceRisk' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-devices.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (26 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/前端/E2E/admin-scope；删除 006 迁移与 `docs/impl/evidence/ADM-API-04c/`。
