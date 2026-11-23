# ER-903 · 开发环境限流开关（ER_RATELIMIT_OFF）

- 状态：done
- 成熟度：dev-only（生产禁用）
- 依赖：ER-001（ratelimit 包）、ER-410（生产配置校验）

## 问题

生产限流阈值是按真实用户行为定的：

| 键 | 阈值 | 位置 |
|---|---|---|
| `login:phone:<手机号>` | 10 次 / 1 小时 | `server/internal/gateway/handler.go:158` |
| `sms:phone:<手机号>` | 1 次 / 1 分钟 | `server/internal/gateway/handler.go:131` |
| `adminlogin:<用户名>` | 5 次 / 10 分钟 | `server/internal/adminhttp/handler.go:79` |
| `email:code:<邮箱>` | 1 次 / 1 分钟 | `server/internal/gateway/email.go:34` |

本地联调会在几十秒内反复登录/发码，直接撞上限，只能重启进程清内存计数。
重启是掩盖问题，不是配置。

## 方案

新增开发用无限流器 `ratelimit.Unlimited`，由 `ER_RATELIMIT_OFF` 显式开启：

1. `server/internal/ratelimit/unlimited.go`：`Allow` 恒放行，`Reset`/`Ping` 恒成功。
2. `server/internal/stack/bootstrap.go` `OpenLimiter`：开关为真且非生产 → 返回 `NewUnlimited()`；
   生产环境 → 返回错误 `stack: ER_RATELIMIT_OFF is forbidden in production`。
2a. `server/internal/stack/stack_pg.go` 新增 `devLimiter()`，`NewDev` / `NewPostgres` /
   `NewPostgresWithRedis` 全部改走它。**这是本卡最容易漏的一处**：装配层原来直接写死
   `ratelimit.NewMemory()`，只改 `OpenLimiter` 对 `cmd/stack` 完全无效——实测 20 次登录
   仍在第 11 次被拒。
3. `server/internal/config/validate.go` `ValidateDeployment`：生产环境出现该变量即拒绝启动，
   与 `ER_ADMIN_TOTP_OFF` 同等对待。
4. `scripts/dev-demo.sh`：本地演示默认带上 `ER_RATELIMIT_OFF=1`。

默认（未设置变量）行为不变：开发用内存限流器、生产用 Redis，阈值保持严格。

## 验收断言 → 测试

| 断言 | 测试 |
|---|---|
| 无限流器任意次数都放行 | `ratelimit.TestUnlimitedAlwaysAllows` |
| 无限流器 Reset/Ping 成功 | `ratelimit.TestUnlimitedResetAndPing` |
| 开发 + 开关 → `*ratelimit.Unlimited`，连打 30 次登录不拒 | `stack.TestDevRateLimitOffReturnsUnlimited` |
| 生产 + 开关 → 启动失败 | `stack.TestProductionRefusesRateLimitOff` |
| 未设开关 → 仍是 `*ratelimit.Memory`（默认严格） | `stack.TestDevWithoutFlagUsesMemoryLimiter` |
| 生产配置校验拒绝该变量 | `config.TestValidateDeploymentRejectsRateLimitOffInProduction` |
| 开发配置校验允许该变量 | `config.TestValidateDeploymentDevAllowsRateLimitOff` |

## 回滚

删除 `ER_RATELIMIT_OFF` 即回到原严格限流；代码层回滚只需删 `unlimited.go` 与
`OpenLimiter` 开头那段分支，其余文件无依赖。
