# 第十轮：审查 S1/S2/S5 + B1 修复 · D1/D2/D3 落地

> 日期：2026-11-20 ｜ 目标轮次 1/20

## 一、修掉上一轮遗留的严重项

| 编号 | 问题 | 修复 | 回归测试 |
|---|---|---|---|
| **S1** | 限流 Lua 只在 `current==1` 设 PEXPIRE，历史无 TTL 的 key 会**永久封禁**该主体 | 改为 `current==1 or ttl<0` 都补设过期；`window<=0`/`limit<=0` 直接报错而非静默关掉防护 | `TestRedisKeyWithoutTTLGetsExpiry`（手工造无 TTL 脏 key，断言被补上 TTL） |
| **S2** | TOTP 重放 key 是全局 step，同一 30 秒只有第一个管理员能登录 | key 改为 `(adminID, step)`；后端故障返回 `SRV_MAINTENANCE` 而非伪装成「重放」 | `TestTwoAdminsSameStepBothLogin`、`TestReplayBackendFailureIsServiceError` |
| **S5** | `Unregister` 异步清在线表，可能误删新连接 presence | presence 值改为 `instance#connGen`；清理改**同步 + 500ms 超时**，并顺带清共享绑定 | `TestStaleUnregisterKeepsNewPresence` |
| **S3** | 共享绑定 `HSet` 后从不 `EXPIRE`，绑定永久驻留 | 改 pipeline：HSet + Expire 同事务 | `TestBindingHasTTL` |
| **B1** | QUIC 可靠队列**无界**，对端不读流时会 OOM；写失败静默 | 改有界队列（容量 256），满时返回 `Backpressure`；写失败计入 `dropped` 统计 | `backpressure_check` 示例：灌 267 帧后命中背压 |

## 二、D1/D2/D3 差异化能力落地

新建 crate `er-features`（纯逻辑，零平台依赖，可完整单测）：

### D1 网络诊断（PRD §5.2.5）

`diagnose(&ProbeReport) -> Diagnosis`：把 NAT 类型、打洞次数、UDP 阻断、运营商限速、丢包翻译成
**结论 + 原因 + 影响 + 建议**四段人话。

覆盖场景（7 条单测）：
- 直连成功且丢包低 → 「P2P 直连成功，链路正常」，无建议
- 直连成功但丢包 8% → 追加 HighLoss 原因与「用有线/5GHz」建议
- 对称 NAT → 原因点明「对称型 NAT 无法直连」，建议放行 UDP 443/3478
- UDP 被阻断 → 独立原因 + 换网建议
- 疑似限速 → 「错峰使用」建议
- 打洞失败 → 结论里带尝试次数与中继延迟增量

### D2 防睡眠（PRD §5.6）

`SleepInhibitor` trait + **RAII `SleepGuard`**：
- `Drop` 释放，保证 panic / 提前 return 也不留「电脑永不睡眠」的残留
- `keep_display` 跟随隐私屏设置（隐私屏开着时没必要阻止息屏）
- 手动 `release_now` 后 Drop 不重复释放

5 条单测覆盖：正常释放、提前 return 释放、手动释放不重复、隐私屏联动、重复 release 幂等。

### D3 会话水印（PRD §5.7）

`layout(&spec, w, h) -> Vec<WatermarkTile>`：
- 不透明度**强制夹在 0.08–0.15**（低于看不见，高于干扰使用）
- 间距不小于 4 倍字号
- 平铺**外扩一圈**，保证画面边缘也有水印（不留空白区被人裁掉）
- 布局确定性（同参数同结果）

6 条单测覆盖：透明度夹取、间距下限、全覆盖、外扩、空帧、确定性。

## 三、验证

| 项 | 结果 |
|---|---|
| Go 单测 | **16 包全绿** |
| Go 竞态 | 16 包全绿 |
| vet / gofmt | 无输出 |
| 集成（内存） | 通过 |
| **Rust** | **55 passed**（原 37 + 新增 18） |
| QUIC 端到端 | 通过 |
| 背压验证 | 267 帧后命中 Backpressure |

## 四、环境阻塞（必须记录）

**Docker Desktop 无法启动**（`Error response from daemon: Docker Desktop is unable to start`），
导致 PostgreSQL 与 Redis 容器不可用，因此：

- `TestPGContract`、`TestRedis*` 系列**本轮未执行**（不是失败，是环境不可用）
- 上一轮这些测试是通过的；本轮代码改动涉及限流 Lua 与绑定 TTL，需要 Docker 恢复后补跑

尝试过：`open -a Docker`、`open -a 'Docker Desktop'` 各等待 25–40 秒，均返回同一错误。

## 五、下一步

1. Docker 恢复后补跑 PG/Redis 契约
2. er-capture 真实实现（macOS ScreenCaptureKit / Windows DXGI）
3. er-codec 真实实现（VideoToolbox / NVENC）
4. Flutter 客户端骨架（六页）
