# ER-411 证据：生产配置一致性

- 状态: 完成（统一校验 + PG+Redis smoke；gate PASS）
- 限制：SMS/Email provider 配置未纳入。

## 1. 变更

| 文件 | 内容 |
|---|---|
| config/validate.go | ValidateDeployment 生产必需项/HTTPS/localhost/TLS/调试开关 |
| config/validate_test.go | 完整/缺项/调试/开发矩阵 |
| cmd/gateway/admin/stack | 启动即校验 |
| stack/pg_redis_smoke_test.go | Docker PG+Redis 生产启动且无 root |

## 2. 规则

- 缺 DATABASE_URL/REDIS_URL/ER_SECRET/三个公开端点任一 → 失败。
- gateway/signal 必须 https 且非 localhost；relay 非 localhost；ER_PUBLIC_TLS=1。
- ER_ADMIN_TOTP_OFF=1 或开发 secret → 失败。
- 开发/测试不受影响。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 完整生产配置通过 | TestValidateDeploymentProductionComplete | PASS |
| 缺任一必需项失败 | TestValidateDeploymentRejectsIncompleteProduction | PASS |
| localhost/http/调试开关失败 | TestValidateDeploymentRejectsDebugSwitchesInProduction | PASS |
| 开发默认通过 | TestValidateDeploymentDevAllowsDefaults | PASS |
| PG+Redis 生产启动且无 root | TestProductionPostgresRedisStarts | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/config/ -run TestValidateDeployment
    PASS
    $ go test ./internal/stack/ -run TestProductionPostgresRedisStarts
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 校验只读环境变量；配置文件/密钥管理系统的注入一致性未覆盖。
- SMS/Email 生产 provider 未实现，故未强制其配置；固定码仍只允许开发 provider。
- cmd 启动校验与 stack.NewPostgres* 内部构造双重入口；测试直接调用 NewPostgres 时不经过 cmd 校验（仍有 seed 保护）。
- PG/Redis 容器测试依赖 docker，CI 需可用。

## 6. 回滚

- 回滚会让生产半配置启动/缺总线运行，禁止。
- 调试开关不得迁移为环境默认。
