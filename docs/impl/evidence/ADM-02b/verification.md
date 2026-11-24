# ADM-02b 验证记录（历史账号 PhoneEnc 回填）

> 卡：`docs/impl/tasks/ADM-02b-phone-enc-backfill.md`
> 时间：2026-11-23 11:58-12:04（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `server/internal/store/store.go` | Store 接口新增 `UpdateAccountPhoneEnc` |
| `server/internal/store/memory.go` | 内存实现（未知账号返回 `ErrNotFound`） |
| `server/internal/store/pg.go` | PG 实现（`UPDATE accounts SET phone_enc=$2 WHERE id=$1`） |
| `server/internal/store/storetest/storetest.go` | 契约用例 `AccountPhoneEncBackfill`（memory/pg 共用） |
| `server/internal/auth/auth.go` | 登录时若 `PhoneEnc == ""` 则幂等回填；失败按存储错误返回，不静默吞 |
| `server/internal/auth/auth_test.go` | `TestLoginBackfillsMissingPhoneEnc` |

## 2. 字段映射

N/A（`phone_mask` 仍由 `maskPhone(PhoneEnc)` 生成，契约未变）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 登录回填 PhoneEnc（返回值 + 存储），重复登录幂等 | `TestLoginBackfillsMissingPhoneEnc` | PASS |
| 存储层回填 + 未知账号 ErrNotFound | `TestMemoryContract/AccountPhoneEncBackfill` | PASS |
| 新账号建号即写 PhoneEnc（ADM-02a 不回归） | `TestSMSLoginStoresPhoneEnc` | PASS |
| 列表脱敏 + HTTP 信封（ADM-02a 不回归） | `TestListUsers_Mask` / `TestListUsers_HTTPEnvelope` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
=== RUN   TestMemoryContract/AccountPhoneEncBackfill
--- PASS: TestMemoryContract/AccountPhoneEncBackfill (0.00s)
=== RUN   TestLoginBackfillsMissingPhoneEnc
--- PASS: TestLoginBackfillsMissingPhoneEnc (0.00s)
--- PASS: TestListUsers_Mask / TestListUsers_Empty / TestListUsers_Forbidden
--- PASS: TestListUsers_HTTPEnvelope
GATE: PASS
```

## 5. 未完成项、限制与风险

- 回填只在**用户下次登录**时发生；长期不登录的历史账号仍显示 `***`。这是有意的：哈希单向，无法离线反推，也不引入明文回填脚本。
- PG 契约用例需要 `DATABASE_URL` 才真正跑；本地无 DSN 时只跑内存实现（CI 有 PG 时会跑同一套用例）。
- 回填失败会让登录返回 `SRV_INTERNAL`（fail-closed），而不是"登录成功但脱敏永远坏着"。

## 6. 回滚

`git checkout -- server/internal/store/store.go server/internal/store/memory.go server/internal/store/pg.go server/internal/store/storetest/storetest.go server/internal/auth/auth.go server/internal/auth/auth_test.go`。
