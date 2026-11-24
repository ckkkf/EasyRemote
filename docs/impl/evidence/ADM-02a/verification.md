# ADM-02a 验证记录（phone_mask 修复）

> 卡：`docs/impl/tasks/ADM-02a-user-phone-mask.md`
> 时间：2026-11-23 11:00-11:10（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/auth/auth.go` | 短信登录建号补 `PhoneEnc: phone`（此前只写 `PhoneHash`，导致后台脱敏拿不到手机号） |
| `server/internal/auth/auth_test.go` | `TestSMSLoginStoresPhoneEnc`：建号写入 + 重复登录不清空 |
| `server/internal/adminapi/adminapi_test.go` | `TestListUsers_Mask` / `TestListUsers_Empty` / `TestListUsers_Forbidden`（补齐 ADM-02-users 缺失断言） |
| `server/internal/adminhttp/users_envelope_test.go` | `TestListUsers_HTTPEnvelope`：code=0 / name=OK / trace_id 非空 / data.items 数组 / phone_mask 脱敏 |

未改：`docs/spec/**`、`httpx`、其它端点、任何前端代码。

## 2. 字段映射

| 字段 | 来源 | 说明 |
|---|---|---|
| `data.items[].phone_mask` | `docs/spec/contract.yaml` `UserRow.phone_mask`；PRD §6.3.2 行 592 | 修复后形如 `139****3000`，由 `maskPhone(PhoneEnc)` 生成 |
| `data.items[].id/status/created_at` | 同上 | 未变 |
| `data.total` | `httpx.Page.Total` | 未变 |
| `code/name/trace_id` | `docs/spec/error-codes.md` §3 | 未变 |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 短信建号写入 PhoneEnc，重复登录不清空 | `TestSMSLoginStoresPhoneEnc` | PASS |
| 列表脱敏符合 `^[0-9]{3}\*{4}[0-9]{4}$` | `TestListUsers_Mask` | PASS |
| 空库返回空切片（可序列化成 `[]`） | `TestListUsers_Empty` | PASS |
| 无 `user.read` 角色被拒（ADMIN_FORBIDDEN） | `TestListUsers_Forbidden` | PASS |
| HTTP 信封 + 脱敏端到端 | `TestListUsers_HTTPEnvelope` | PASS |
| 既有信封契约 | `TestAdminEnvelopeContract` | PASS（38 条对齐） |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 端到端前后对比

```text
修复前: items: [('acc-55fd24f6b5eaf40a', "'***'")]
修复后: items: [('acc-bbf31bb36bd7f104', "'139****3000'")]
```

## 5. 未完成项、限制与风险

- **历史账号**：只存了 `PhoneHash`（HMAC 单向），无法反推明文，列表仍显示 `***`。回填需要给 Store 增加 `UpdateAccountPhoneEnc` 并改 memory/pg 两个实现，已拆 **ADM-02b**（下次登录幂等回填）。
- 卡内原断言「readonly 且无 `user.read`」与现行权限表不符：`adminapi.go:34` 里 readonly **有** `user.read`。本卡改用未知角色验证拒绝路径，并保留注释说明，未改权限表。
- 本卡未改前端；`ADM-UI-00` 矩阵 A3 行已加变更记录说明该缺陷与修复。

## 6. 回滚

`git checkout -- server/internal/auth/auth.go server/internal/auth/auth_test.go server/internal/adminapi/adminapi_test.go`，删除 `server/internal/adminhttp/users_envelope_test.go`。
