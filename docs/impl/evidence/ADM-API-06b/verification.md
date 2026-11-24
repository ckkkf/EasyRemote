# ADM-API-06b 验证记录（客户端版本与灰度）

> 卡：`docs/impl/tasks/ADM-API-06b-versions.md`
> 时间：2026-11-23 14:00-14:12（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/stack/migrations/007_client_versions.sql` | `client_versions` 表（platform+version 唯一） |
| `store/{store,memory,pg}.go`、`storetest` | `ListVersions/CountVersions/GetVersion/UpsertVersion/UpdateVersionStatus` |
| `docs/spec/contract.yaml`、生成物、`route_specs.go` | `ClientVersionRow`/`VersionPublishRequest`/`VersionRetireRequest` + ADM-23/24/25 |
| `adminapi/adminapi.go`、`versions_test.go` | `ListVersions/CountVersions/PublishVersion/RetireVersion` + 覆盖率一致性校验 |
| `adminhttp/handler.go` | 三条路由（GET/POST 同路径分流，method 由全局 methodguard 保证） |
| `web-admin/tests/admin-scope.test.ts` | 白名单加 /versions，移出"禁止出现"名单 |

## 2. 字段映射

| 字段 | 来源 / 规则 |
|---|---|
| `platform` | 枚举 windows/macos/android/ios |
| `status` | draft / rolling / full / retired（retired 只能由 retire 进入） |
| `rollout_percent` | draft=0，rolling=1–99，full=100（服务端强制） |
| `force_update` / `notes` | 强制升级开关与更新日志 |
| `platform+version` | 唯一；重复发布=更新策略，ID 不变 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（53 端点） |
| 幂等覆盖/排序/状态/未知 id | `TestMemoryContract/ClientVersions` | PASS |
| 校验/审计/权限 | `TestVersionPublishAndRetire` | PASS |
| 信封契约 | `TestAdminEnvelopeContract` | PASS（含三条新路由） |
| web 回归 | `npm test -- --run` | PASS 55/55 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（53 个类型 / 53 条接口）
✓ 路由 method/path/auth 与契约一致（53 条端点）
--- PASS: TestMemoryContract/ClientVersions
--- PASS: TestVersionPublishAndRetire
--- PASS: TestAdminEnvelopeContract
GATE: PASS
```

## 5. 过程中修掉的问题

- 首次采集用 `go test -run 'ClientVersions|...'` 时，`TestMemoryContract` 顶层名不匹配导致子测试被跳过（不是失败）。已用 `-run 'TestMemoryContract/ClientVersions'` 单独补跑并把输出追加进证据。

## 6. 未完成项、限制与风险

- **A11 页面未做**（另开 UI 卡）；`admin-scope.test.ts` 仍守护导航里没有版本管理入口。
- **客户端检查更新接口未做**：服务端有版本数据，但客户端无更新流程；在客户端实现前，版本数据不会被真实消费。
- 白名单灰度（按账号/设备）未做，需要白名单表与投放算法，另卡。
- 覆盖率目前是"策略值"，服务端不计算实际命中比例（需要客户端上报生效版本）。

## 7. 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 007 迁移、`versions_test.go` 与 `docs/impl/evidence/ADM-API-06b/`。
