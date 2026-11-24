# ADM-API-04b 验证记录（在线筛选 + 强制下线 / 解绑）

> 卡：`docs/impl/tasks/ADM-API-04b-device-actions.md`
> 时间：2026-11-23 12:30-12:44（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | 新增 ADM-12/13 + `DeviceActionRequest` |
| 三端生成物、`route_specs.go` | 生成 + 显式 method/auth 声明 |
| `store/{store,memory,pg}.go`、`storetest` | `Page.Online` 过滤；`DeleteDevice` |
| `adminapi.go`、`adminapi_test.go` | `ForceOffline`（关会话+离线+审计）、`UnbindDevice`（删除+审计）、权限表新增两项 |
| `adminhttp/handler.go` | 两条路由 + `online` 查询参数 |
| `web-admin` 三文件 | 在线筛选下拉、行内「强制下线/解绑」+ 原因对话框、详情页去掉重复死入口 |
| `scripts/admin-ui-e2e-devices.py` | 20 条 E2E |
| `web-admin/tests/admin-scope.test.ts` | 白名单同步新增端点 |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| ADM-12 force-offline | `DeviceActionRequest{device_id, reason}` | `OkResult` |
| ADM-13 unbind | 同上 | `OkResult` |
| GET devices | 新增 query `online=1|0`（不进 schema） | `list<DeviceRow>` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约 + 路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（40 端点） |
| online 过滤 + 删除语义 | `TestMemoryContract/DeviceDeleteAndOnlineFilter` | PASS |
| 强制下线关会话 + 离线 + 审计 | `TestForceOfflineClosesSessionsAndMarksOffline` | PASS |
| 原因/权限/未知设备 | `TestForceOfflineRequiresReasonAndPermission` | PASS |
| 解绑权限 + 删除 + 双审计 | `TestUnbindDeviceDeletesAndAudits` | PASS |
| 端到端 | `TestDevicesE2E` | PASS 20/20 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 路由 method/path/auth 与契约一致（40 条端点）
--- PASS: TestForceOfflineClosesSessionsAndMarksOffline
--- PASS: TestUnbindDeviceDeletesAndAudits
PASS online filter returns online devices
PASS force offline marks device offline
PASS unbind removes device -> 2 -> 1
E2E OK (20 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 过程中发现并修正

- 生成物曾出现字段丢失（Overview/UserRow 回退）：重新执行 `gen-contract` 后恢复；提醒后续每次生成后检查关键字段，而不是只看"类型数一致"。
- `admin-scope.test.ts` 的端点白名单是刻意摩擦：新增 ADM-12/13 后测试立即变红，同步更新后通过。

## 6. 未完成项、限制与风险

- **标记风险设备未做**：需要设备风险列 + 解除流程（ADM-API-04c）。
- 强制下线只关闭该设备参与的会话并把设备表标记离线；真正撤销信令租约需要 presence 层接口（当前 `Presence` 只读）。
- 解绑删除设备行，但不清理 trust edges（表里可能留下悬空信任），属后续清理卡。
- 详情页不再提供强制下线入口，避免与列表页语义分叉。

## 7. 回滚

`git checkout --` 契约/生成物/route_specs/store/adminapi/handler/前端/E2E/admin-scope；删除 `docs/impl/evidence/ADM-API-04b/`。
