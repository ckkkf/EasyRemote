# EasyRemote 代码与功能质量审查 · 2026-11-24

## 结论与边界

本轮抽查了远控建连、授权/关闭、数据面入口、后台查询、API 错误边界和测试闸门。**不建议把远控标为 stable**：根因分布在客户端、会话协调、生产存储和传输，不能用“接口返回成功”或“某次出现了首帧”代替整条链路通过。没有逐页核对后台原型像素，也没有逐文件安全审计。本轮保留已有工作树变更，不改 AGENTS、PRD、公开契约、迁移和测试基线。

## 已修复

| 优先级 / 任务 | 根因和影响 | 验证 |
|---|---|---|
| P1 / [ER-907](tasks/ER-907-session-request-expiry.md) | 申请防重先读取全局 500 条，遗漏同设备对时可能由原子写入分支返回旧会话；关闭失败也会被吞掉。改为只用设备对原子结果，按 CAS 检查过期并收口，再创建新会话。没有延长授权窗口或放宽验权。 | 内存复现 requested/authorized、并发重试、激活竞态和写失败；真实 PostgreSQL 验证过期授权、新会话、旧状态和 outbox。见 [状态机](../../server/internal/session/session.go#L103)。 |
| P1 / [ER-908](tasks/ER-908-postgres-session-list.md) | PG 会话 SELECT 有 12 列、旧代码只解码 8 列，非空列表失败；调用方包括后台会话查询和离线清理器。列表和单条读共用解码器；测试改用真实迁移和隔离 schema。 | 修前真实 PG 报 `got 12 and 8`；修后真实 PG 的 `SessionLifecycle` 验证授权时间、版本、双方就绪标志及关闭分类。[PG 列表](../../server/internal/store/pg.go#L833) |
| P2 / [ER-909](tasks/ER-909-quality-percentile-parity.md) | PG 插值和 Memory 最近秩导致同一组采样显示不同的 P50/P95；PG 对齐共享契约。 | 四条样本从 PG 的 25/39 改为两种存储一致的 20/40；窗口无采样断言保留。[PG 统计](../../server/internal/store/pg.go#L1867) |

## 未修复项：按风险排序

| 优先级 | 发现 / 证据 | 下一张最小卡与验收 |
|---|---|---|
| P1 | **票据签发检查与会话生命周期不一致。** [ticket handler](../../server/internal/gateway/handler.go#L765) 检查状态和设备凭证，但没有对仍标记为 `authorized` 的记录检查 `AuthExpireAt`。这是代码审查发现的签发前校验缺口；未做现网验证，不等于已证实越权接管。 | 单独关闭策略卡：先确定 active 重连策略，再由服务端状态机统一判定签发资格。定向回归验证 pending、过期、closed 不发票，有效授权仍可用，角色和同账号免密边界不变。 |
| P1 | **取消不等于对端取消。** [连接页](../../client/lib/pages/connect_page.dart#L145) 在不知道会话 ID 时只退出等待并丢弃迟到结果，服务端仍可能生成申请。 | 给连接尝试建立可撤销标识或明确请求结果归属；延迟 GW-06 响应后“取消→立刻重连”，验证旧授权撤下、新会话不被误关，双方最终状态一致。 |
| P1 | **主动过期、状态送达与线上生命周期未闭合。** [Janitor](../../server/internal/session/janitor.go#L29) 只扫 active、一次最多 [200 条](../../server/internal/session/session.go#L376)，并以双方在线租约判断；[Dispatcher](../../server/internal/session/dispatcher.go#L19) 有实现和单测，当前没有生产启动调用。单独验证纯主控端在线租约以及 Redis 故障后的事件重投递；不把“事务里已写 outbox”认作“已通知双方”。 | 拆为到期扫描、生产事件投递两卡。用真实 PG/Redis 双实例验证分页、授权超时、在线或掉线边界、下游暂时失败后重投递，以及两端退出、输入停止和票据失效。 |
| P1 | **桌面入口与协议基线有分叉。** [被控端](../../core/crates/er-client/src/agent.rs#L276) 和[主控端](../../core/crates/er-client/src/agent.rs#L896) 仍直接构造 RelayClient；其它路径或回环测试走通 P2P，不等于该入口走同一种协商。 [协议 §1](../spec/protocol.md#1-传输基线) 要求可丢媒体与可靠有序控制分离。 | 先做真实调用链 ADR，再按入口迁到 `er-transport`。跨两台设备验证 P2P/Relay fallback、丢包/重排、视频拥塞下断开和控制可靠性；保留安全回滚，不因兼容而放宽授权。 |
| P2 | **桌面画面呈现与指标尚未闭合。** [Flutter IO](../../client/lib/session_video_io.dart#L291) 分配并拷贝 RGBA、Dart 解码；[会话小窗](../../client/lib/pages/session_page.dart#L1298) 显示网关 RTT，帧率/码率/链路明确标为未接入。 | 原生纹理、呈现帧计量分开验收。双平台收集首帧和呈现数据，检查失败/断开释放；未接入的指标不伪造。 |
| P2 | **后台 API 解析与页面组织有维护风险。** [API 入口](../../web-admin/src/api.ts#L76) 先无保护解析 JSON，遇非 JSON 网关响应抛原生异常；非信封成功体可被断言成 DTO。[App.tsx](../../web-admin/src/App.tsx) 约 2,955 行。已有 React 和本地 shadcn 风格的 Radix/Tailwind 组件；本轮未证明各页符合原型。 | 优先补真实 fetch 边界：HTML/空体错误、非法成功信封、完整业务错误及 trace。然后按页面拆模块并复用生成 DTO；依据 pages.md 和原型做浏览器级验收，非 SSR 快照。 |

## 回滚

每张卡可分别撤销对应方法和新测试，不需要回滚数据库迁移。ER-908 和 ER-909 都触及 `pg.go` 的不同方法，按对应方法还原，不整文件覆盖。全量命令和本轮限制见报告末尾。

## 验收命令与实测结果

- `cd server && go test -race ./internal/session ./internal/gateway -count=1`：两包通过。定向复跑四条新回归、Memory 的列表和质量统计均通过。
- `cd server && go test -race -tags integration ./internal/store -run '^TestPG(Contract|Ping)$' -count=1 -v`：临时本地 PostgreSQL 15、生产迁移；`SessionLifecycle`、`ExpiredRequestReplacement`、`QualityStatsSince`、完整契约和连通测试均通过，最终 `PASS`。每个子测试使用独立 schema，容器已清理。
- 真实临时 Redis 验证 `./internal/signal`，Pub/Sub、在线租约和跨实例授权投递通过。组合调用时向整个 `./internal/stack` 注入了 `REDIS_URL`，使期望未配置 Redis 的 `TestDevWithoutFlagUsesMemoryLimiter` 红；隔离 Redis 环境后，使用临时 PG 复跑 `env -u REDIS_URL go test -race ./internal/stack -count=1 -v`，生产 PG、PG+Redis 启动和默认限流行为均通过。测试环境隔离仍可单独增强。
- `bash scripts/gate.sh`：20 步通过；Flutter 127 条、Web 58 条通过，最终行 **`GATE: PASS`**。PG 存储契约的 `integration` tag 尚未接入默认 gate/CI，本轮单独实跑，不将未来跳过计为通过。
- `python3 scripts/mac/e2e_desktop_entry.py`：macOS 单机双进程、原生 FFI 真采集回归；取消发生在已知会话 ID 后，拒绝结果返回、首帧解码和关闭传播通过。关键输出：`FFI_FIRST_FRAME 1920x1080 bytes=8294400`、`ER906_NATIVE_CROSS_PROCESS_PASS`。这不是 GUI/跨机器验收，也不是帧率或延迟基准。
- `git diff --check`：通过。

未验证：Windows ↔ macOS 双机 GUI、在途请求取消的服务端闭环、真实复杂 NAT/带宽与长时间资源增长、后台原型逐页验收、生产发布。没有修改部署或生成新安装包。

## 后续修复

- [ER-910](tasks/ER-910-relay-ticket-authorization.md)：修复票据签发校验；仍在授权窗口内的会话可领/换票，新票据不会超出授权窗口。主动关停已建立的数据面不在本卡。
- [ER-911](tasks/ER-911-cancel-connect-before-session-id.md)：在途取消只有设计、未实现。现有设备对复用让迟到 ID 无法证明尝试归属；需审批新契约后再改。
- [ER-912](tasks/ER-912-native-harness-signal-readiness.md)：修复原生测试把设备绑定误当作信令就绪的竞态；使用实际 Hub 在线探针。
