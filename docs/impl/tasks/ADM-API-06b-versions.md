# ADM-API-06b 客户端版本与灰度（A11 后端）

> 来源：`docs/impl/tasks/ADM-API-06-risk-version-admin-contract.md`（A11 版本管理）
> 需求：`docs/spec/pages.md` A11（行 254）、PRD §6.3.6（行 621-623）
> 前置：ADM-API-06 缺口卡、迁移目录修复（ADM-API-02d）

## 目标

落地 A11 的后端：版本列表（版本号/平台/状态/覆盖率）、灰度发布（覆盖率与状态一致）、强制升级开关、更新日志、下线（原因必填）。所有写操作进审计。

**不做**：A11 页面（另开 UI 卡）、客户端"检查更新"接口（客户端尚无更新流程，需独立设计）、白名单灰度（需要账号/设备白名单表，另卡）。

## 输入 / 输出

| 端点 | 请求 | 响应 | 权限 |
|---|---|---|---|
| ADM-23 `GET /admin/v1/versions` | 分页 | `list<ClientVersionRow>` | `version.read` |
| ADM-24 `POST /admin/v1/versions` | `VersionPublishRequest` | `ClientVersionRow` | `version.write`（super/ops） |
| ADM-25 `POST /admin/v1/versions/retire` | `{version_id, reason}` | `OkResult` | `version.write` |

## 校验规则（服务端强制）

| 状态 | 覆盖率 |
|---|---|
| draft | 必须 0 |
| rolling | 1–99 |
| full | 必须 100 |
| retired | 只能通过 retire 接口进入 |

平台限 `windows/macos/android/ios`；`platform+version` 唯一（重复发布=更新策略字段，ID 不变）。

## 必须通过的断言

- [ ] `gen-contract --check` + `check-routes --strict`：53 条端点一致。
- [ ] `TestMemoryContract/ClientVersions`：幂等覆盖保持 ID、列表按平台排序、状态更新、未知 id `ErrNotFound`。
- [ ] `TestVersionPublishAndRetire`：非法平台 / draft 带覆盖率 / full 不足 100 均 `ADMIN_CONFIG_INVALID`；正常发布写审计；下线缺原因 `ADMIN_REASON_REQUIRED`；readonly 写被拒、读允许。
- [ ] `TestAdminEnvelopeContract`：新路由信封对齐。
- [ ] web 55 tests 与 `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`、三端生成物、`route_specs.go`
- `server/internal/stack/migrations/007_client_versions.sql`（新增）
- `server/internal/store/{store,memory,pg}.go`、`storetest`
- `server/internal/adminapi/adminapi.go`、`versions_test.go`
- `server/internal/adminhttp/handler.go`
- `web-admin/tests/admin-scope.test.ts`
- `docs/impl/tasks/ADM-API-06b-versions.md`、`docs/impl/evidence/ADM-API-06b/**`

禁止：手改生成物；允许 full 状态只有部分覆盖率；下线不写审计。

## 验收命令

```bash
node scripts/gen-contract.mjs --check && node scripts/check-routes.mjs --strict
cd server && go test ./internal/store/ ./internal/adminapi/ ./internal/adminhttp/ -run 'ClientVersions|VersionPublish|Envelope' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && bash scripts/gate.sh
```

## 完成标准

五条断言全绿；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约/生成物/store/adminapi/handler/admin-scope；删除 007 迁移、`versions_test.go` 与 `docs/impl/evidence/ADM-API-06b/`。
