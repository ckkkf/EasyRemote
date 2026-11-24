# ADM-API-04b 设备在线筛选 + 强制下线 / 解绑

> 来源：`docs/impl/tasks/ADM-API-04-device-contract.md`（筛选：在线状态；操作：强制下线、解绑）
> 需求：`docs/spec/pages.md` A5（行 226-227）、PRD §6.3.3（行 598）
> 前置：ADM-API-04a（设备元信息）

## 目标

1. `GET /admin/v1/devices?online=1|0` 服务端过滤（presence 权威字段）；
2. `POST /admin/v1/devices/force-offline`：关闭该设备参与的未结束会话 + 标记离线，原因必填、写审计；
3. `POST /admin/v1/devices/unbind`：删除设备记录，原因必填、写审计，仅超级管理员。

**不做** 标记风险：需要设备风险列与解除流程，单独排期（ADM-API-04c）。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-12 `POST /admin/v1/devices/force-offline` | `DeviceActionRequest{device_id, reason}` | `OkResult` | `device.force_offline`（super/ops/cs） |
| ADM-13 `POST /admin/v1/devices/unbind` | 同上 | `OkResult` | `device.unbind`（仅 super） |

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：40 条端点一致。
- [ ] `TestMemoryContract/DeviceDeleteAndOnlineFilter`：online 过滤只返回在线；删除后取不到、重复删除 `ErrNotFound`。
- [ ] `TestForceOfflineClosesSessionsAndMarksOffline`：会话变 closed、设备 offline、审计 `device.force_offline`。
- [ ] `TestForceOfflineRequiresReasonAndPermission`：空原因 `ADMIN_REASON_REQUIRED`、readonly `ADMIN_FORBIDDEN`、未知设备 `CONN_DEVICE_NOT_FOUND`。
- [ ] `TestUnbindDeviceDeletesAndAudits`：ops 被拒、super 成功、设备删除、审计含 denied + ok 两条。
- [ ] `TestDevicesE2E`：在线筛选、强制下线（原因门控 + 真离线 + 审计）、解绑（设备数 -1 + 审计）、详情页无重复死入口。
- [ ] 六屏视觉判据不变；`bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`server/internal/adminhttp/route_specs.go`
- `server/internal/store/{store,memory,pg}.go`、`storetest/storetest.go`
- `server/internal/adminapi/adminapi.go`、`adminapi_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/src/api.ts`、`App.tsx`、`features/devices/device-detail-page.tsx`
- `scripts/admin-ui-e2e-devices.py`
- `docs/impl/tasks/ADM-API-04b-device-actions.md`、`docs/impl/evidence/ADM-API-04b/**`

禁止：手改生成物；把强制下线做成只改本地状态；解绑不写审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ ./internal/adminhttp/ -run 'Contract|ForceOffline|Unbind|ListDevices' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-devices.py
bash scripts/gate.sh
```

## 完成标准

七条断言全绿；E2E `E2E OK (20 checks)`；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E；删除 `docs/impl/evidence/ADM-API-04b/`。
