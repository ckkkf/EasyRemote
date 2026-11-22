# ER-903 Chaos 矩阵

日期：2026-11-22。可自动执行子集见 scripts/chaos/run.sh。

| 故障 | 期望降级/恢复 | 证据 | 结果 |
|---|---|---|---|
| 信令实例离线 | presence TTL 过期→离线 | signal presence TTL 测试 | PASS |
| 会话未授权超时 | requested→closed(request_expired) | session Timeout/Expire 测试 | PASS |
| 授权窗口过期 | authorized 不可 ready | AuthWindow 测试 | PASS |
| Relay 重注册/早到数据 | pending 队列，不误当视频 | er-session 单元测试 | PASS |
| 弱网丢包/抖动/重排 | 会话仍建立 | lossy_e2e_test | PASS |
| Redis 重启 | 在线/撤销键重建，无永久在线 | **未运行** | 未完成 |
| PG 重启 | 事务/会话恢复 | **未运行** | 未完成 |
| Relay 进程重启 | 双端重连/重新注册 | **未运行** | 未完成 |
| 磁盘满 | 上传/审计失败可观察 | **未运行** | 未完成 |
| 时钟偏差 | 超时/过期语义一致 | 仅单元时钟注入 | 部分 |

## 僵尸 active 风险

- 当前 active 会话没有心跳续租/看门狗；进程崩溃后数据库可能残留 active。
- ER-903 要求“无僵尸 active”，需增加 active 心跳与 janitor；当前**未实现**，不能宣称完成。

## 命令

    bash scripts/chaos/run.sh
    # 外部故障需 docker/真实 Redis/PG/relay，当前环境未提供。
