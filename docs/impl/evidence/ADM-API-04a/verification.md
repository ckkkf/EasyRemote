# ADM-API-04a 验证记录（设备元信息）

> 卡：`docs/impl/tasks/ADM-API-04a-device-meta.md`
> 时间：2026-11-23 12:20-12:32（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `migrations/004_device_meta.sql` | 幂等加 4 列（默认空串，老行无需回填） |
| `server/internal/store/{store,memory,pg}.go` | Device 加 4 字段；`UpdateDeviceMeta`（空值不覆盖）；PG 列/扫描/插入同步 |
| `storetest/storetest.go` | 契约用例 `DeviceMeta`（含部分上报不覆盖） |
| `server/internal/gateway/handler.go` | bind 接受可选元信息并持久化 |
| `bind_platform_test.go` | 未知字段用例改用真正拼错的字段（`device_name` 已成合法字段） |
| `server/internal/adminapi/adminapi.go` | ListDevices 输出四个字段 |
| `web-admin` 三文件 | 列表 10 列 + 详情 kv + 说明 |
| `scripts/admin-ui-e2e-devices.py` | 元信息上报 → 列表/详情断言 |

## 2. 字段映射

| 字段 | 类型 | 语义 |
|---|---|---|
| `DeviceRow.device_name/os_version/client_version/nat_type` | string | 客户端 bind 上报；空串=未上报 |
| `BindRequest.device_name/os_version/client_version/nat_type` | string? | 可选；空值保留旧值 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三端契约一致 | `gen-contract --check` | PASS |
| 写入 / 部分上报不覆盖 / 未知设备 | `TestMemoryContract/DeviceMeta` | PASS |
| 未知字段仍被拒绝 | `TestBindMalformedBodyIsRejected` | PASS |
| 列表 10 列 + 值来自 bind + 详情 11 组 kv | `TestDevicesE2E` | PASS 16/16 |
| 视觉判据 | `TestVisualBaselineUnchanged` | 仅 a6 kvPairs 7→11（有意） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
--- PASS: TestMemoryContract/DeviceMeta (0.00s)
--- PASS: TestBindMalformedBodyIsRejected (0.00s)
PASS device columns -> ['设备码','设备名','归属账号','平台','系统版本','客户端版本','NAT','在线','最后在线','操作']
PASS device names from bind -> ['夹具工作站','夹具测试机']
PASS device detail kv includes metadata -> 11
E2E OK (16 checks)
a6-device-detail.kvPairs: 7 -> 11     # 有意变化
GATE: PASS
```

## 5. 未完成项、限制与风险

- **客户端尚未真正上报**：Flutter 端 bind 请求没带这些字段，真机设备仍显示「—」。服务端路径已通（E2E 用 HTTP 上报验证），客户端补齐属客户端卡。
- 只写非空值：客户端某次漏报不会清空已有元信息（有单测覆盖）。
- 在线状态筛选、强制下线/解绑/标记风险、30 天质量曲线仍缺（ADM-API-04 剩余 + ADM-API-02）。
- 列表变 10 列，1360 宽度仍不溢出（响应式审计会继续盯着）。

## 6. 回滚

`git checkout --` 契约/生成物/store/gateway/adminapi/前端/E2E；删除 004 迁移与证据目录。
