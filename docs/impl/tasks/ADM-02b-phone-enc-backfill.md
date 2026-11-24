# ADM-02b 历史账号 PhoneEnc 回填

> 来源：ADM-02a 遗留项（`docs/impl/tasks/ADM-02a-user-phone-mask.md`「历史数据」一节）
> 需求：PRD §6.3.2 行 592「手机号默认脱敏（138****8888）」、`docs/spec/contract.yaml` `UserRow.phone_mask`
> 前置：ADM-02a（新账号建号已写 PhoneEnc）

## 目标

历史账号只存了 `PhoneHash`（HMAC 单向），无法反推明文。用户下次登录时手上就有明文手机号，因此在登录路径**幂等回填** `PhoneEnc`，让后台 `phone_mask` 从 `***` 恢复成 `138****8888`。

## 输入 / 输出

不改 API、不改契约、不改生成物；字段映射 **N/A**（`phone_mask` 的来源仍是 `maskPhone(PhoneEnc)`）。

## 必须通过的断言

- [ ] `TestLoginBackfillsMissingPhoneEnc`（`server/internal/auth/auth_test.go`）：预置只有 `PhoneHash` 的账号 → 登录后返回值与存储的 `PhoneEnc` 都被回填；再次登录幂等。
- [ ] `TestMemoryContract/AccountPhoneEncBackfill`（`server/internal/store/storetest`）：`UpdateAccountPhoneEnc` 写入成功；未知账号返回 `ErrNotFound`（不静默成功）。
- [ ] `TestSMSLoginStoresPhoneEnc`（ADM-02a）不回归：新账号建号即写 `PhoneEnc`。
- [ ] `TestListUsers_Mask` / `TestListUsers_HTTPEnvelope`（ADM-02a）不回归。
- [ ] `bash scripts/gate.sh` 全绿。

## 只准改这些文件

- `server/internal/store/store.go`（接口新增 `UpdateAccountPhoneEnc`）
- `server/internal/store/memory.go`、`server/internal/store/pg.go`
- `server/internal/store/storetest/storetest.go`
- `server/internal/auth/auth.go`、`server/internal/auth/auth_test.go`
- `docs/impl/tasks/ADM-02b-phone-enc-backfill.md`、`docs/impl/evidence/ADM-02b/**`

禁止：改 `docs/spec/**`、`httpx`、其它端点；禁止把回填失败静默吞掉。

## 验收命令

```bash
cd server && go test ./internal/store/ -run 'Contract' -v && go test ./internal/auth/ ./internal/adminapi/ ./internal/adminhttp/ -v
cd .. && bash scripts/gate.sh
```

## 完成标准

四条断言全绿；gate `GATE: PASS`；证据落 `docs/impl/evidence/ADM-02b/`。

## 回滚

`git checkout -- server/internal/store/store.go server/internal/store/memory.go server/internal/store/pg.go server/internal/store/storetest/storetest.go server/internal/auth/auth.go server/internal/auth/auth_test.go`。
