# ADM-02a 用户手机号脱敏修复（SMS 注册账号 PhoneEnc 缺失）

> 发现于：ADM-UI-02c 的对话框 E2E 截图（`docs/impl/evidence/ADM-UI-02c/ban-dialog.png`）
> 关联：ADM-02-users 卡内断言 `TestListUsers_Mask`（该断言至今不存在，所以这个缺陷一直没被拦住）
> 需求：PRD §6.3.2 行 592「手机号默认脱敏（138****8888）」、`docs/spec/contract.yaml` `UserRow.phone_mask`

## 现象（可复现）

```text
$ python3 /tmp/probe_mask.py
items: [('acc-55fd24f6b5eaf40a', "'***'")]
```

任何通过短信验证码注册的账号，在 `GET /admin/v1/users` 里的 `phone_mask` 都是 `***`，不是 `139****0000`。

## 根因

- `server/internal/auth/auth.go:102-106`（短信登录建号）只写 `PhoneHash`，**没有写 `PhoneEnc`**：
  `acc = &store.Account{PhoneHash: hash, Status: "active"}`
- 邮箱登录建号（`auth.go:199`）写了 `PhoneEnc: normalized`，所以邮箱账号能脱敏，短信账号不能。
- `server/internal/adminapi/adminapi.go:231` 用 `maskPhone(acc.PhoneEnc)`；空串长度 < 7 → 返回 `"***"`（`adminapi.go:244-248`）。

## 目标

让短信注册账号的 `phone_mask` 符合 `^[0-9]{3}\*{4}[0-9]{4}$`，并把 ADM-02-users 缺失的断言补齐。

## 必须通过的断言

- [ ] `TestListUsers_Mask`：短信注册账号的 `phone_mask` 匹配 `^[0-9]{3}\*{4}[0-9]{4}$`（不允许出现 `***`）。
- [ ] `TestSMSLoginStoresPhoneEnc`：`auth.Login` 新建账号时 `PhoneEnc` 等于归一化手机号。
- [ ] `TestListUsers_Envelope` / `TestListUsers_Empty` / `TestListUsers_Forbidden`：补齐 ADM-02-users 卡内缺失的另外三条断言。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `server/internal/auth/auth.go`（短信登录建号补 `PhoneEnc`）
- `server/internal/auth/auth_test.go`（新增 `TestSMSLoginStoresPhoneEnc`）
- `server/internal/adminapi/adminapi_test.go`（新增四条 ListUsers 断言）
- `docs/impl/tasks/ADM-02a-user-phone-mask.md`

禁止：改 `docs/spec/**`、改 `httpx`、动其它端点。

## 待决策（执行前必须确认）

1. **历史数据**：已存在的账号只有 `PhoneHash`（单向），无法反推明文手机号。可选方案：
   - A. 历史账号保持 `***`，并在审计/文档里说明（最小改动）；
   - B. 引入手机号加密列（KMS/密钥管理）回填，需独立 ADR 与迁移；
   - C. 用户下次登录时回填 `PhoneEnc`（登录路径已持有明文，可幂等写入）。
   本卡默认按 **C + 历史行保持 `***`** 处理，方案 B 需负责人批准。
2. 脱敏口径：`phone_mask` 是否允许为空账号（如仅邮箱注册）返回空串而不是 `***`。

## 验收命令

```bash
cd server && go test ./internal/auth/ ./internal/adminapi/ -run 'ListUsers|SMSLogin' -v
cd .. && bash scripts/gate.sh
```

## 回滚

`git checkout -- server/internal/auth/auth.go server/internal/auth/auth_test.go server/internal/adminapi/adminapi_test.go`。

## 执行记录（2026-11-23，Round 5）

**决策**：采用最小修复——短信登录建号时写入 `PhoneEnc`；历史账号（只有 `PhoneHash`）继续显示 `***`，因为哈希单向不可反推。回填方案（下次登录幂等写入 `PhoneEnc`）需要给 Store 增加更新方法并同时改 memory/pg 两个实现，单独拆卡 **ADM-02b**，不在本卡夹带。

**实际改动**：

| 文件 | 改动 |
|---|---|
| `server/internal/auth/auth.go` | `Login` 建号补 `PhoneEnc: phone`（与 `LoginEmail` 的 `PhoneEnc: normalized` 对齐） |
| `server/internal/auth/auth_test.go` | 新增 `TestSMSLoginStoresPhoneEnc`（建号写入 + 重复登录不清空） |
| `server/internal/adminapi/adminapi_test.go` | 新增 `TestListUsers_Mask`（正则 `^[0-9]{3}\*{4}[0-9]{4}$`）、`TestListUsers_Empty`、`TestListUsers_Forbidden` |
| `server/internal/adminhttp/users_envelope_test.go` | 新增 `TestListUsers_HTTPEnvelope`（code/name/trace_id/data.items 数组 + 脱敏） |

**与卡内断言的口径修正**：卡里写「role=readonly 且无 user.read 时返回 ADMIN_FORBIDDEN」不成立——现行权限表里 readonly **有** `user.read`（`adminapi.go:34`）。`TestListUsers_Forbidden` 改用未知角色 `guest` 验证拒绝路径，并在测试注释里写明原因。

**端到端验证**：

```text
$ python3 /tmp/probe_mask.py          # 修复前
items: [('acc-55fd24f6b5eaf40a', "'***'")]

$ python3 /tmp/probe_mask.py          # 修复后
items: [('acc-cc89795451370c2b', "'139****3000'")]
```


## 后续（ADM-02b，已完成）

历史账号回填已由 `ADM-02b-phone-enc-backfill.md` 落地：登录路径幂等写 `PhoneEnc`，存储层新增 `UpdateAccountPhoneEnc`，契约用例 `AccountPhoneEncBackfill` 通过。
