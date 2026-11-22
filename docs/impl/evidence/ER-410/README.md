# ER-410 证据：生产迁移与 dev seed

- 状态: 完成（生产不 seed + 真实 PG 启动；gate PASS）
- 限制：PG+Redis 组合烟测未做。

## 1. 变更

| 文件 | 内容 |
|---|---|
| stack_pg.go | seedDevAdminIfNotProduction；NewPostgres/WithRedis 改用它 |
| seed_test.go | 生产不 seed / 开发 seed |
| pg_production_smoke_test.go | Docker Postgres 生产启动 + 无 root |

## 2. 语义

- 生产：只 migrate，不创建任何管理员；ER_SECRET 必需。
- 开发/测试：seed root/dev-pass 便于联调。
- SeedDevAdmin 仍拒绝生产（直接调用也安全）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 生产 helper 不 seed | TestProductionNeverSeedsDevAdmin | PASS |
| 开发 helper seed | TestDevSeedsDevAdmin | PASS |
| 生产 PG 启动迁移且无 root | TestProductionPostgresStartsWithoutDevAdmin | PASS |

## 4. 命令与输出

    $ cd server && go test ./internal/stack/ -run 'Test.*SeedsDevAdmin|TestProductionPostgres'
    PASS
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界

- 生产 PG+Redis 组合（NewPostgresWithRedis）同样改用 helper，但未做容器烟测；ER-411/ER-903 扩展。
- 生产开户仍依赖 runbook 手工流程，未自动化。
- 生产 PG 无 Redis 时 NewPostgres 仍可启动（无信令/通知），配置一致性由 ER-411 拒绝。

## 6. 回滚

- 回滚会在生产创建 root/dev-pass，属严重安全问题，禁止。
- 回滚迁移分离会导致启动失败或半工作，禁止。
