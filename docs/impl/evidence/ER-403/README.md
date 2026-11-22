# ER-403 证据：凭证随机性与锁定

- 状态: 完成（CSPRNG + 轮换/长度/锁定测试；gate PASS）

## 1. 变更

| 文件 | 内容 |
|---|---|
| er-client/Cargo.toml | 新增 getrandom |
| conductor.rs | 临时密码改 CSPRNG 8 位数字；随机源失败返回空串 |
| device/credential_test.go | 验证码格式/轮换/随机源/锁定/密码校验 |

## 2. 语义

- 设备码 9 位、crypto/rand、冲突重试。
- 验证码 6 位、10 分钟、一次性、5 次错误锁 30 分钟。
- 临时密码 6..16 字母数字；服务端仅存 HMAC；不消费不轮换。
- host_password：8 位数字 CSPRNG；失败不回退弱随机。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 临时密码 8 位数字且两次不同 | host_password_is_random_digits | PASS |
| 验证码 6 位、TTL 10m、轮换旧码失效 | TestVerificationCodeFormatAndRotation | PASS |
| 连续 64 次无重复（随机源） | TestVerificationCodeRandomSource | PASS |
| 密码长度/字符集/哈希/校验 | TestFixedPasswordValidation | PASS |
| 5 次锁定/过期/重放（既有） | TestVerifyCodeWrongLocksAfterFive / Expired / SuccessConsumes | PASS |

## 4. 命令与输出

    $ cd core && cargo test -p er-client host_password
    ok
    $ cd server && go test ./internal/device/
    ok
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 账号短信/邮箱固定码（123456/654321）仅开发 provider；生产必须注入真实通道（ER-410/ER-411 配置校验）。
- 设备码/验证码生成失败路径用错误码返回；host password 随机源失败返回空串，由上层长度校验拒绝。
- 6 位验证码空间 1e6，已有 5 次锁定与 10 分钟周期；账号级速率限制仍归 ER-411/ER-902。

## 6. 回滚

- 回滚 CSPRNG 会恢复可预测密码，禁止。
- 回滚锁定/一次性消费会允许爆破/重放，禁止。
