# ADM-API-04a 设备元信息（device_name / os_version / client_version / nat_type）

> 来源：`docs/impl/tasks/ADM-API-04-device-contract.md`（列：设备名、系统版本、客户端版本、NAT）
> 需求：`docs/spec/pages.md` A5（行 225）、A6（行 231）、PRD §6.3.3（行 597）
> 前置：ADM-API-03b（同款迁移 + 契约先行流程）

## 目标

1. 设备表加四列（幂等迁移），契约 `DeviceRow` 与 `BindRequest` 暴露这些字段；
2. 客户端**绑定时上报**（`/v1/devices/bind` 可选字段），服务端只写非空值、不清空旧值；
3. 管理端设备列表与详情展示真实值，「—」表示尚未上报。

## 输入 / 输出

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `DeviceRow.device_name/os_version/client_version/nat_type` | string | 客户端上报的设备元信息；空串=未上报 | 契约 `DeviceRow` |
| `BindRequest.device_name/os_version/client_version/nat_type` | string? | 可选上报；空值保留旧值 | 契约 `BindRequest` |

## 必须通过的断言

- [ ] `gen-contract --check` 三端一致。
- [ ] `TestMemoryContract/DeviceMeta`：四字段写入；只上报一个字段时其余保留；未知设备 `ErrNotFound`。
- [ ] `TestBindMalformedBodyIsRejected`（更新后）：真正未知字段仍被 `DisallowUnknownFields` 拒绝（原用例用 `device_name` 当未知字段，现已是合法字段）。
- [ ] `TestDevicesE2E`：列表 10 列（设备名/系统/客户端/NAT）、设备名与系统版本来自 bind、详情 kv 11 组含 OS/客户端/NAT。
- [ ] `TestVisualBaselineUnchanged`：与 ADM-API-03b 基线相比只有 a6 `kvPairs 7→11`（有意）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物
- `migrations/004_device_meta.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`、`storetest/storetest.go`
- `server/internal/gateway/handler.go`、`bind_platform_test.go`
- `server/internal/adminapi/adminapi.go`
- `web-admin/src/api.ts`、`App.tsx`、`features/devices/device-detail-page.tsx`
- `scripts/admin-ui-e2e-devices.py`
- `docs/impl/tasks/ADM-API-04a-device-meta.md`、`docs/impl/evidence/ADM-API-04a/**`

禁止：手改生成物；用前端默认值冒充设备名；把某次漏报当作"清空设备信息"。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/store/ ./internal/gateway/ ./internal/adminapi/ -run 'Contract|Bind|ListDevices' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-devices.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-API-04a/screens --compare-to docs/impl/evidence/ADM-API-03b/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；视觉差异只有 a6 kv 组数；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 004 迁移与证据目录。
