# ADM-API-03a 用户列表 device_count（契约先行）

> 来源：`docs/impl/tasks/ADM-API-03-user-contract.md`（列：设备数）
> 需求：`docs/spec/pages.md` A3（行 213：设备数）、PRD §6.3.2（行 588）
> 前置：ADM-API-02a（同款 contract-first 流程）

## 目标

契约先行给 `UserRow` 增加 `device_count`，服务端一次聚合（避免 N+1），管理端用户列表与详情显示真实设备数。

**不做** `nickname`/`last_login_at`：账号表没有这两列，需要 schema 迁移，单独排期（已记在 ADM-API-03）。

## 输入 / 输出

| 字段 | 类型 | 语义 | 来源 |
|---|---|---|---|
| `data.items[].device_count` | int | 该账号已绑定设备数 | 契约 `UserRow`；服务端 `DeviceCountsByAccount` 一次聚合 |

## 必须通过的断言

- [ ] `node scripts/gen-contract.mjs --check`：三端一致。
- [ ] `TestMemoryContract/DeviceCountsByAccount`：多账号计数正确；无设备账号不出现在结果；空入参返回空 map。
- [ ] `TestListUsers_DeviceCount`：acc1 两台设备 → 2，acc2 无设备 → 0。
- [ ] `TestUsersE2E`：列表列变为 6 列且「设备数」为服务端值（夹具 2 / 1）。
- [ ] `TestVisualBaselineUnchanged`：与 ADM-UI-10 基线相比**只有 a3 列变化**（有意）。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `docs/spec/contract.yaml`
- `server/internal/contract/types_gen.go`、`web-admin/src/contract.ts`、`client/lib/contract.dart`（生成物）
- `server/internal/store/store.go`、`memory.go`、`pg.go`、`storetest/storetest.go`
- `server/internal/adminapi/adminapi.go`、`adminapi_test.go`
- `web-admin/src/api.ts`、`web-admin/src/App.tsx`（用户列表列）、`web-admin/src/features/users/user-detail-page.tsx`
- `scripts/admin-ui-e2e-users.py`
- `docs/impl/tasks/ADM-API-03a-user-device-count.md`、`docs/impl/evidence/ADM-API-03a/**`

禁止：手改生成物；用前端列表长度冒充设备数；把昵称/最近登录编出来。

## 验收命令

```bash
node scripts/gen-contract.mjs --check
cd server && go test ./internal/store/ ./internal/adminapi/ -run 'Contract|ListUsers' -v
cd ../web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && python3 scripts/admin-ui-e2e-users.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-API-03a/screens --compare-to docs/impl/evidence/ADM-UI-10/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；视觉差异只有 a3 列；gate `GATE: PASS`。

## 回滚

`git checkout --` 契约、三端生成物、store 四个文件、adminapi 两个文件、前端三个文件、E2E 脚本。
