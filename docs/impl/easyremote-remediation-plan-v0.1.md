# EasyRemote 现有实现审查与整改总纲 v0.1

> 审查日期：2026-11-22
> 适用基线：`docs/prd/easyremote-prd-v0.8.md`、`docs/spec/*`
> 执行对象：DeepSeek 或人工开发者
> 本文是整改总纲，不是可直接编码的任务卡。执行前必须把对应条目拆成 `docs/impl/tasks/<ID>.md`，一次只做一张卡。

## 1. 目标与结论

本轮审查聚焦“代码是否真的形成可用、安全、可观测、可回归的远控闭环”，不以页面数量、模块数量或单元测试数量判断完成度。

结论：当前项目已有较多服务、页面、crate 和测试骨架，但生产主路径没有闭环。最高风险集中在：

1. 生产网关无法可靠把远控申请送达被控端。
2. 被控端核心自动同意授权，且“允许被控”默认开启，违反 PRD 安全红线。
3. 主控在授权完成前同步阻塞接管，靠固定 sleep 对齐两端，易冻结 UI 或首次连接必败。
4. `active`、主动断开、后台强制中断没有打通数据面和双方通知。
5. 现行协议要求 WebTransport/P2P/Relay/可靠控制流，实际主路径是自定义裸 UDP Relay。
6. 媒体链路被限制到约 12 FPS，Flutter 每帧跨 FFI 搬 RGBA，无法满足 1080p60 目标。
7. 多项设置、质量指标、文件/剪贴板和后台提示呈现为“可用”，但真实副作用不成立或语义错误。
8. 当前本地 gate 能通过，但未覆盖 Rust、Flutter、真实 E2E、性能和安全；Flutter 测试当前已有失败。

因此不建议继续在现有 UI 上补样式或局部重试。应先收紧能力暴露与安全默认值，再按“控制面 -> 会话状态 -> 传输 -> 媒体 -> 功能通道 -> 管理后台 -> 发布闸门”重建闭环。

## 2. 审查方法与当前验证快照

### 2.1 已检查范围

- Go：gateway、adminhttp/adminapi、session、signal、stack、device、relay、store、集成测试。
- Rust：er-client、er-session、er-transport、er-codec、er-ffi、er-features、er-channels。
- Flutter：登录/设备/连接/会话/设置/文件、API、偏好、会话存储、FFI 视频与 host service。
- React：后台 API、仪表盘、用户/设备/会话/审计/配置、登录与登出。
- 工程：contract/error 生成器、route checker、gate、GitHub Actions。

### 2.2 命令快照

| 命令 | 结果 | 说明 |
|---|---|---|
| `bash scripts/gate.sh --report` | PASS | 只证明现有 Go/生成物/路径级检查通过，不代表远控闭环通过 |
| `cd web-admin && npm run build` | PASS | Vite 构建通过 |
| `cd client && flutter analyze` | FAIL | 111 个 issue，包含未使用字段/导入与大量 lint 债务 |
| `cd client && flutter test` | FAIL | `native_test.dart` 的“ABR 决策可跨边界返回”断言失败；两条真实 E2E 因缺环境变量被跳过 |
| `cd core && cargo test --workspace` | 未运行 | 当前本机没有可用 `cargo`，不能把 CI 配置当成本地通过证明 |

严格 `bash scripts/gate.sh` 必须在本文修改完成后再次执行；上述失败项是后续整改的真实输入，不得用 baseline 或 skip 消除。

## 3. 主要发现

### 3.1 P0：安全与主路径阻断

| ID | 发现 | 代码证据 | 直接后果 | 对应卡 |
|---|---|---|---|---|
| A-01 | 生产申请通知未装配 | `server/internal/stack/build.go:31-41` 未注入 Notifier；`stack/notify.go:8-39` 仅进程内全局 Hub；`server/cmd/stack/main.go:41-64` 的数据库分支未挂信令 | 被控端收不到申请，主控永久等待 | ER-110/111 |
| A-02 | 被控端自动授权 | `core/crates/er-client/src/host_stream.rs:138-165` 收到 session 后直接 `target.consent` | 跨账号可绕过本地明确授权 | ER-101/102 |
| A-03 | 安全总开关默认开启 | `client/lib/prefs_store_types.dart:5-12,72-74` 默认/缺省均为 true；`device_list_page.dart:69-82` 登录后自动启动 host | 首次安装即暴露远控能力，违反 `pages.md:141-154` | ER-100 |
| A-04 | 接管存在授权竞态且阻塞 UI | `session_page.dart:80-112` 未等待授权即同步 attach；`session_video_io.dart:96-129` 同步 FFI；`ctrl_session.rs:221-253` 固定 sleep 与阻塞重试 | 首次连接易失败，桌面 UI 可卡住数秒到十几秒 | ER-201/402/504 |
| A-05 | `active` 状态没有生产写入 | `server/internal/session/session.go:131-164` 定义 `Activate`，生产调用链未调用；客户端把 authorized 当成已连 | 后台活跃数错误、UI 状态虚假、生命周期不闭合 | ER-200/201 |
| A-06 | 断开/强制中断只改局部状态 | Flutter 断开仅离开页面；`adminapi` kill 只更新存储；`web-admin/src/App.tsx:814-825` 却承诺双方立即收到事件 | 对端仍采集/控制，旧票据和线程可能继续工作 | ER-202/203 |
| A-07 | 生产启动路径自相矛盾 | `server/internal/stack/stack_pg.go:117-164` 生产 PG 构造仍调用 `SeedDevAdmin`，而 `:90-104` 在生产拒绝执行 | `ER_ENV=production` + DATABASE_URL 可能直接无法启动 | ER-410 |

### 3.2 P1：架构、实时性和功能闭合度

| ID | 发现 | 代码证据 | 直接后果 | 对应卡 |
|---|---|---|---|---|
| A-08 | 在线状态不是实时 presence | `device/device.go:82-130` 绑定即写 online=true；`signal/hub.go:79-92` Redis TTL 只在注册时写，心跳未续租 | 离线设备长期显示在线；长连接超过 TTL 又可能被判离线 | ER-112 |
| A-09 | 传输实现偏离冻结协议 | `er-client` 直接使用 `RelayClient`；`er-session/relay_client.rs:46-149` 是裸 UDP/自定义分片；协议要求见 `docs/spec/protocol.md:10-18` | 无 P2P/ICE、控制不可靠、浏览器路线不可复用 | ER-500-504 |
| A-10 | UDP 分片与注册确认不可靠 | `relay_client.rs:54-75` 单分片预算 8192；`:94-118` 发送成功即视作注册成功 | IP 分片/丢包放大，假连接，弱网不可控 | ER-501/504 |
| A-11 | 媒体管线无法达到目标 | `ctrl_session.rs:183-207` 主动限到约 12 FPS；`session_video_io.dart:131-171` 每帧分配、复制 RGBA 并 Dart 解码；页面 60ms 轮询 | 高 CPU/内存带宽、累积延迟、达不到 1080p60 | ER-600-603 |
| A-12 | 会话质量数据是伪指标 | `session_page.dart:884-978` FPS/码率为破折号、链路固定“已授权”；`:1018-1071` 延迟/丢包来自 gateway ping | 用户无法诊断真实媒体链路，后台统计失真 | ER-603/800 |
| A-13 | 隐私屏/防休眠未接入主路径 | 设置页只写 prefs；`er-features/d2_power.rs` 只有抽象和 fake 测试；现有 privacy 是涂黑采集帧 | UI 开关无效；隐私屏语义与 PRD 相反 | ER-701/702 |
| A-14 | 文件与剪贴板不是远控数据通道 | Flutter 文件页走 gateway 文件 API；`er-channels` 仅模块/单测，未接入会话 | 用户看到“远程文件/剪贴板”，实际未传给对端 | ER-710/711 |
| A-15 | endpoint/端口硬编码且不一致 | `session_page.dart:104-110`、`device_list_page.dart:109-123` 固定 127.0.0.1:19000；主入口默认端口又不同 | 非本机部署无法工作，开发/发布行为漂移 | ER-400 |
| A-16 | FFI 输入字符串泄漏 | `session_video_io.dart:108-191`、`host_service_io.dart:129-203` 多次 `toNativeUtf8` 未释放 | 长会话/高频输入持续增长 native 内存 | ER-402 |

### 3.3 P1/P2：契约、后台和工程闸门

| ID | 发现 | 代码证据 | 直接后果 | 对应卡 |
|---|---|---|---|---|
| A-17 | 契约只校验 path 且大量 `any` | `contract.yaml:191-208` GW-06/07/08 response=any；GW-08 规定 POST+bearer，实际 `handler.go:380-393` 是 query GET+device；route checker 仍通过 | 三端漂移无法被 gate 发现 | ER-010/011 |
| A-18 | handler 普遍缺 method guard | 多条路由直接 `HandleFunc`，由 body/query 决定行为 | 错误 method 可进入业务逻辑，契约与安全边界模糊 | ER-012 |
| A-19 | 本地 gate 覆盖不完整 | `scripts/gate.sh` 不跑 Rust、Flutter、web build/E2E/性能/安全；禁止模式仅与 baseline 比较 | gate PASS 与可发布无关 | ER-001/002 |
| A-20 | E2E 被跳过且调试测试混入 | Flutter 两条 E2E 缺 `ER_GATEWAY_URL/ER_RELAY_ADDR` 时 skip；`zz_dbg_test.dart` 等参与正式测试 | CI 无法证明真实主路径 | ER-900 |
| A-21 | 管理后台身份和动作语义失真 | `App.tsx:1330-1345` 硬编码 root/超级管理员且登出只清本地 token；`:814-825` 中断承诺不成立 | 权限展示错误、服务端 session 未撤销、运维误判 | ER-810/811 |
| A-22 | 后台列表是假分页 | 前端对已被后端固定上限截断的数组 `slice`；服务端 collect 使用 100/200/500 上限 | total、筛选和分页不可信 | ER-801 |
| A-23 | 凭证使用明文文件/localStorage | `client/lib/session_store_io.dart` 写 `~/.easyremote/session.json`；Web/后台 token 在 localStorage | 本机恶意进程/XSS 可直接窃取长期凭证 | ER-401/811 |
| A-24 | WebSocket Origin 全放行 | `stack/rendezvous.go:49`、`cmd/rendezvous/main.go:56` 使用 `CheckOrigin: true` | 浏览器环境扩大跨站 WebSocket 攻击面 | ER-411 |

## 4. 目标架构

### 4.1 控制面

```text
Flutter/React
  -> generated contract client
  -> Gateway/Admin HTTP
  -> session state machine + transaction/outbox
  -> shared signal bus (Redis Streams/PubSub with observable delivery)
  -> signal instance owning target connection
  -> target local consent controller
  -> consent result event
  -> session state machine
```

关键不变量：

- HTTP 成功只表示对应领域动作成功，不表示后续异步动作已成功。
- 通知投递失败必须让 request 失败或进入明确可重试失败态，不能伪装 `requested`。
- presence 是有租约的连接事实，不是设备表属性。
- 会话状态由单一状态机管理，所有入口（HTTP、signal、data plane、admin）都发 command/event，不直接改字段。
- 状态变更与 outbox 同事务；重复事件按 event_id/session version 幂等。

### 4.2 数据面

```text
er-client session orchestrator
  -> er-transport::Transport
       -> candidate gathering (host/srflx/relay)
       -> P2P race with 3s deadline
       -> WebTransport session
          - media datagram
          - reliable control stream
          - reliable data stream
  -> crypto/auth handshake
  -> host media pipeline / controller render pipeline
  -> data-plane ready event -> server session active
```

上层只依赖 Transport 接口和通道语义。Relay 是候选/兜底实现，不是 er-client 的具体依赖。输入、心跳、关闭和权限收回走可靠高优先级通道，不与视频拥塞共命运。

### 4.3 客户端状态

```text
idle
-> resolving
-> auth_pending
-> consent_pending
-> negotiating
-> connected
-> reconnecting
-> closed
\-> failed(error + retry policy)
```

Flutter 只渲染 controller 的状态流。所有按钮调用 command；Timer 只允许做展示倒计时，不可驱动真实状态迁移。

### 4.4 媒体和指标

- Host：平台捕获 -> 硬编优先 -> 有界最新帧队列 -> media datagram。
- Controller：datagram -> 抖动/丢帧策略 -> 硬解优先 -> 原生 texture -> Flutter `Texture`。
- 统计在真实执行点采集：capture/encode/send/receive/decode/present/input-ack。
- 端到端延迟使用帧时间戳与时钟偏差处理；链路类型来自 Transport；丢包来自媒体序列号；FPS 来自 present。

## 5. 执行规则

1. 严格按阶段顺序执行。前一阶段 exit gate 未通过，不进入下一阶段。
2. 每个下列 ER 卡先落成独立任务卡，控制在约 100 行，列字段映射、文件白名单、失败测试和回滚。
3. 每张卡先提交失败测试，再提交最小实现；禁止一次 PR 横跨 Go/Rust/Flutter/React 四端的大爆炸重写。
4. 架构卡必须先做 ADR/Spike；Spike 只能产出数据和决策，不能偷偷变成生产实现。
5. P0 安全修复优先于兼容旧错误行为。必要时通过迁移和 feature flag 处理，但默认 fail-closed。
6. 当前不满足闭环的入口先禁用/标 experimental；不得等待完整重构后再收口风险。
7. 所有 evidence 放 `docs/impl/evidence/<CARD-ID>/`，包括命令输出、结构化 benchmark、E2E 日志和必要截图；禁止提交 secret/用户画面。

## 6. 分阶段整改计划

### Phase 0：止血与可验证基线

目标：先让“不完整功能不能伪装完成”，并让本地/CI 能发现真实失败。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-000 | 建立 capability maturity/feature flag，禁用未闭环的远控、隐私屏、文件、剪贴板和伪质量指标 | client、server config、web-admin | `TestIncompleteCapabilityIsDisabled`；发布构建不可绕过 |
| ER-001 | 让 `scripts/gate.sh` 覆盖 Go race、Rust fmt/clippy/test、Flutter format/analyze/test、web typecheck/test/build | scripts、CI | `TestGateMatchesCIJobs`；故意制造各栈失败时 gate 必红 |
| ER-002 | 自动启动真实测试环境，禁止 E2E 因缺 env skip | scripts、compose/test harness | `TestE2EHarnessStartsAndStopsServices`；两条现有 E2E 不再 skip |
| ER-003 | 清理调试测试并修复当前 ABR 失败 | client/test、相关 FFI | `native_test.dart` 全绿；无 `zz_*` 正式测试 |
| ER-004 | 建立硬编码/空 catch/固定 sleep/未释放 FFI 指针的扫描规则 | scripts | 每类各有 checker 自测；存量形成明确清零清单，不得新增 |

Phase 0 exit gate：严格 gate 可在干净机器一键运行；所有 skip 均有显式批准且不计完成；未闭环入口默认不可用。

### Phase 1：契约与 HTTP 收口

目标：让 contract 真正约束 method/auth/request/response/errors，消除 `any` 和三端手写漂移。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-010 | 将 route checker 升级为 method+path+auth 语义检查 | scripts、生成物 | `TestRouteCheckerRejectsMethodDrift`、`...AuthDrift` |
| ER-011 | 按端点逐卡移除 `response: any` 并生成三端 DTO/client | contract、generator、三端调用 | 每个端点有字段级 schema test；生成物 check 全绿 |
| ER-012 | 为所有 handler 增加 method guard 和统一 405/契约错误 | gateway/adminhttp | 每条路由 wrong-method 表驱动测试 |
| ER-013 | 修正 GW-08 等已知漂移，统一身份凭证语义 | contract first、server、Flutter | GET/POST、bearer/device 只保留 spec 定义；跨设备访问拒绝 |
| ER-014 | Flutter/React 完整保留 Envelope 错误字段 | generated client、错误 UI | trace_id 可复制；retryable 控制重试按钮；非 Map data 不被静默丢弃 |

Phase 1 exit gate：contract 不含完成态 `any`；每条生产路由均能由生成物验证 method/auth/request/response/errors。

### Phase 2：安全默认值、授权与信令送达

目标：先恢复“被控端本机决定是否允许远控”的安全边界，并保证生产通知可送达。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-100 | `allow_control` 默认 false，缺省数据安全迁移，关闭时 host 不启动 | Flutter prefs/host controller | `TestAllowControlDefaultsOff`、`TestDisabledHostRejectsRequest` |
| ER-101 | 实现 C18 本地授权 controller/UI，展示控制者、30s、接受/拒绝 | Flutter、signal adapter | `TestConsentAccept`、`Reject`、`Timeout`、`RemoteCannotDecide` |
| ER-102 | 删除 host_stream 自动 consent，只有本地 decision command 可授权 | er-client、FFI | `TestHostNeverAutoConsents`；无 UI 决策时 0 帧/0 输入 |
| ER-110 | 设计并实现生产 ConsentNotifier/共享 Bus，移除进程内 devHub 生产语义 | stack、signal、gateway | 两网关+两 signal 实例跨进程送达测试 |
| ER-111 | notifier 缺失/投递失败/目标离线时 request fail-closed | gateway、session、errors | `TestConnectRequestFailsWhenNotifierUnavailable` 等 |
| ER-112 | presence 租约：注册、心跳续租、断开撤销、TTL 过期 | signal、device view | `TestPresenceRefreshesOnHeartbeat`、`DisconnectOffline`、`CrashExpires` |
| ER-113 | WebSocket Origin allowlist、read limit、认证 deadline | rendezvous/stack | 非法 Origin、超限消息、未认证超时均断开 |

Phase 2 exit gate：跨账号请求只有被控端本机接受后才可取得数据面票据；生产多实例通知和在线状态有确定性测试。

### Phase 3：会话状态、激活、关闭与重连

目标：建立一个权威、可过期、可审计、可向双方传播的生命周期。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-200 | 把 session 状态机改成 command+version，补非法迁移和并发 CAS | session/store | 全迁移表、并发 accept/kill、重复事件幂等测试 |
| ER-201 | 数据面双方认证成功并 ready 后触发 Activate | transport event -> server | `TestSessionActiveOnlyAfterBothPeersReady`；authorized 不计 active |
| ER-202 | 主动关闭领域事件：双方停止采集/输入、撤票、写 reason/end time | server/signal/er-client/Flutter | 任一端断开 3s 内双方 closed，资源计数归零 |
| ER-203 | 后台 kill 走同一关闭命令并收到双方 ACK/超时结果 | adminapi/signal/admin UI | `TestAdminKillDisconnectsBothPeers`；UI 不再提前宣称成功 |
| ER-204 | 主动清理 consent/auth 超时和僵尸会话，禁止 Request 复用过期 session | session worker/store | 时钟驱动测试；超时后新 request 得新 session |
| ER-205 | 重连模型：会话内授权复用、ticket 轮换、旧路径失效 | session/transport | 拔网 5s，10s 内恢复；旧 ticket replay 拒绝 |

Phase 3 exit gate：后台 active/pending/closed 与真实数据面一致；任一关闭入口均能终止双方并释放资源。

### Phase 4：运行时配置、身份与凭证安全

目标：去掉本机硬编码和可预测/明文凭证，使开发、测试、生产配置可验证且 fail-closed。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-400 | 统一 RuntimeConfig：gateway/signal/STUN/relay/WebTransport/TLS | contract/config endpoint、Flutter、Rust | 发布构建扫描不到业务硬编码 localhost；端口一致性测试 |
| ER-401 | 持久设备身份与 token 使用系统安全存储，定义迁移/注销擦除 | client、er-client identity | 重启身份不变；注销后不可恢复；权限/Keychain 失败 fail-closed |
| ER-402 | 修复全部 FFI 所有权与 UI 阻塞调用 | Dart FFI、er-ffi | ASAN/长时测试无持续泄漏；UI heartbeat 不被 attach 阻塞 |
| ER-403 | 验证码/固定密码使用 CSPRNG并符合 PRD 周期、长度、锁定 | conductor/device/auth | 随机源测试、轮换、5 次锁定、过期和重放测试 |
| ER-410 | 拆分生产迁移与 dev seed，生产启动不创建已知管理员 | stack/cmd/runbook | `TestProductionNeverSeedsDevAdmin`；PG/Redis 生产 smoke 启动 |
| ER-411 | 强制生产 TLS/WSS/HTTP3 与证书校验，开发明文显式 opt-in | config/clients/serve | production 对 http/ws 配置启动失败；证书错误不可忽略 |

Phase 4 exit gate：同一发布包可由受校验配置连接真实环境；生产没有默认账号、明文长期凭证、可预测密码或隐式不安全降级。

### Phase 5：传输层重建

目标：按冻结协议把 er-transport 变成真实主路径，实现 P2P、Relay 兜底和可靠控制/数据通道。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-500 | WebTransport Spike/ADR：wtransport vs quinn+h3，量化握手/带宽/延迟 | er-transport/examples、evidence | PRD 指标：开销 <3%、延迟增量 <2ms，或记录达不到及选定退路 |
| ER-501 | 定义 Transport/Session/Channel trait 和取消/统计接口 | er-transport | 内存 fake 只用于 trait test；er-client 不再依赖 RelayClient |
| ER-502 | 实现 candidate gathering、STUN/ICE 打洞、3s race、Relay fallback | er-transport、server relay/rendezvous | NAT 矩阵测试；P2P 成功/失败切 Relay；链路类型可观测 |
| ER-503 | WebTransport 通道映射：media datagram、control uni stream、data bidi stream | er-transport/er-client | 控制可靠有序；视频丢包不阻塞控制；数据背压生效 |
| ER-504 | 弱网与重连：丢包/抖动/乱序/断网/Relay 重启 | lossyproxy/test harness | PRD 重连阈值；无固定 sleep；无无限队列/线程泄漏 |

Phase 5 exit gate：生产 er-client 只通过 er-transport；P2P/Relay 与通道可靠性均由跨进程测试证明。未达到前远控保持 experimental。

### Phase 6：媒体、渲染、输入和真实质量指标

目标：实现低延迟有界管线，以真实指标驱动 UI 和 ABR。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-600 | Host 捕获/硬编能力矩阵与降级链 | er-capture/er-codec/host | Windows/macOS 实机 1080p60 capture+encode；能力不足明确降档 |
| ER-601 | Controller 硬解与 Flutter texture 零拷贝渲染 | er-codec/er-ffi/Flutter | 无每帧 Dart RGBA 搬运；present FPS 与 native 统计一致 |
| ER-602 | 有界队列、丢旧帧、关键帧请求、抖动策略 | er-client/codec | 30 分钟无累积延迟，内存稳定，丢包后可恢复 |
| ER-603 | 真实质量 telemetry 与 UI：fps/e2e/bitrate/loss/link/encode/decode | transport/media/server/client/admin | 禁止 gateway ping 冒充；每字段可追溯到采集点 |
| ER-604 | ABR 以真实吞吐、RTT、loss、buffer、encode queue 驱动 | er-client/link | 当前失败 ABR 用例修复；升降档、滞回、跨边界 property test |
| ER-610 | 输入可靠流、坐标映射、权限门控与 ACK 延迟 | er-input/transport/client | 键鼠顺序一致；拒绝/closed 后输入 100% 无效；P95 延迟证据 |

Phase 6 exit gate：PRD 固定场景连续 30 分钟无花屏/延迟增长；实际达不到目标的平台按能力降档且 UI 显示真实值。

### Phase 7：被控端本地安全能力与辅助通道

目标：完成本机可见、不可被远程篡改的安全控制，并接通真正的远端数据功能。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-700 | C19 本地常驻控制条、一键断开、远端不可关闭 | platform window/Flutter/host | 会话期间始终可见；本地断开 3s 内双方结束 |
| ER-701 | 隐私屏按平台控制本机物理显示，采集保持 | er-features/platform | 本机黑屏但远端持续有帧；快捷键立即退出并断开 |
| ER-702 | 防休眠接系统 API，并遵守隐私屏联动语义 | er-features/host | acquire/release 实机测试；崩溃恢复无永久锁 |
| ER-710 | 剪贴板走 data stream，大小/格式/敏感内容/开关生效 | er-channels/transport/Flutter | 双向同步、关闭时零传输、过大/敏感错误码正确 |
| ER-711 | 文件传输走 data stream，支持流控、暂停、恢复、冲突策略、校验 | er-channels/transport/Flutter | 真正 peer-to-peer/relay transfer；断线续传与 hash 验证 |

Phase 7 exit gate：设置开关都能在真实 OS/对端产生可验证副作用；关闭后不能从远端重新打开。

### Phase 8：管理后台与运营真实性

目标：后台展示和操作完全基于权威状态，不再用固定身份、截断列表或提前成功文案。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-800 | overview/质量指标改读权威 presence/session/telemetry | adminapi/web-admin | online、active、P2P、延迟与 E2E 场景一致 |
| ER-801 | 后端分页/筛选/排序与真实 total | contract/store/adminapi/web | 数据量超过 500 时无丢项；游标/页码并发一致性测试 |
| ER-810 | 登录返回并展示真实 admin identity/role/permissions | contract/admin/web | 不再硬编码 root；readonly 不展示/不可调用写操作 |
| ER-811 | logout 撤销服务端 session，收紧 token 存储与 CSP | adminsession/web | 登出后的 token 立即无效；XSS/CSP 回归测试 |
| ER-812 | kill UI 等待领域结果，显示 ACK/部分失败/超时和 trace_id | adminapi/web | 文案不承诺未发生事件；审计与实际结果一致 |

Phase 8 exit gate：后台所有数字能通过测试 fixture 和真实 E2E 对账；所有写操作以服务端最终结果为准。

### Phase 9：发布、性能、安全与故障演练

目标：把“可以运行”提升为“可以持续发布且回归可见”。

| 卡号 | 目标 | 主要范围 | 必须通过的验收 |
|---|---|---|---|
| ER-900 | 跨进程桌面 E2E：登录、绑定、申请、授权、P2P/Relay、输入、断开 | harness/CI | Windows+macOS 关键矩阵，无 skip |
| ER-901 | 性能基准自动化并保存原始证据 | benchmark/evidence/CI scheduled | PRD P50/P95、FPS、资源、30min soak 阈值 |
| ER-902 | 安全测试：授权绕过、重放、越权、Origin、日志/凭证泄漏 | security tests | 每条红线有负向测试；依赖/secret scan 全绿 |
| ER-903 | Chaos：signal/Redis/PG/Relay 重启、网络切换、磁盘满、时钟偏差 | harness | 明确降级/恢复，无僵尸 active、无永久在线 |
| ER-904 | Release gate 与回滚演练 | scripts/workflows/runbook | 一键发布前检查；版本兼容；上一版可回滚且迁移可逆/前滚 |

Phase 9 exit gate：严格 gate、桌面 E2E、性能、安全、soak 和 chaos 全绿后，才允许把远控成熟度改为 stable。

## 7. 必须先写的 ADR/设计

以下设计未确认前，DeepSeek 不得直接改生产主路径：

1. ADR-001：WebTransport Rust 实现选型、证书/ALPN、浏览器兼容和 fallback。
2. ADR-002：Transport trait、通道优先级、取消和 telemetry 接口。
3. ADR-003：session command/event/version、outbox、过期 worker 和幂等键。
4. ADR-004：signal bus 投递语义、presence 租约、跨实例路由和故障恢复。
5. ADR-005：Flutter texture/平台 codec 所有权与线程模型。
6. ADR-006：设备身份、账号 token、长期密码的安全存储与迁移。
7. ADR-007：隐私屏各平台可行性；不支持的平台如何 fail-closed。

ADR 必须包含：现状证据、候选方案、权衡、威胁/失败模型、性能数据、决策、回滚、被否决方案。不得只写“采用 X”。

## 8. 关键 E2E 场景清单

每个场景必须保存结构化事件时间线，至少含 trace_id、session_id、状态迁移、链路和关闭原因。

1. 新安装默认不允许被控；请求得到不泄露设备存在性的统一错误。
2. 同账号首次连接：目标在线 -> 本地授权 -> P2P -> 首帧 -> 输入 -> 本地断开。
3. 跨账号：正确验证码仍必须本地授权；拒绝/超时后主控 0 帧、0 输入。
4. 错误验证码连续 5 次锁定；锁定期间正确码也失败；到期恢复。
5. 目标离线、notifier 不可用、signal 实例宕机：主控得到明确可重试错误，不进入假等待。
6. 两网关/两 signal 实例跨节点申请和授权。
7. P2P 成功；P2P 3s 失败自动 Relay；会话中链路变化保持状态一致。
8. 授权后 negotiation 失败：session 进入失败/closed，不留 authorized 僵尸。
9. 主控主动断开、被控本地断开、后台 kill、进程崩溃：双方资源和状态均收口。
10. 拔网 5s 后恢复；旧 ticket 重放失败；恢复后无重复输入。
11. 2%/5% 丢包、抖动、乱序下视频可降档，输入和断开仍可靠。
12. 30 分钟 1080p60：无花屏、无持续内存增长、无累积延迟。
13. 隐私屏：本机不可见，远端持续；本地快捷键退出并断开。
14. 剪贴板/文件开关关闭时不产生任何 data stream 消息；开启时真实到达对端。
15. admin logout token 立即失效；readonly 无写权限；kill 结果与双方事件一致。

## 9. 指标与发布阈值

指标必须来自数据面或权威状态机，禁止从 UI 轮询次数推导。

| 指标 | 采集点 | MVP 阈值/要求 |
|---|---|---|
| 请求送达率 | outbox -> target ACK | 100% 可归因：成功或明确失败，不能悬空 |
| 授权绕过 | target decision audit | 0 |
| active 准确率 | data-plane ready vs server state | 100% 一致 |
| 关闭收敛 | close command -> 双方 stopped | 3s 内，超时有明确结果 |
| P2P 命中率 | Transport selected path | 观测目标 >=70%，按网络分层 |
| 呈现 FPS 达成率 | renderer present | PRD >=95%，局域网目标 >=98% |
| E2E latency | capture timestamp -> present | 按 PRD 场景 P50/P95 |
| 内存增长 | 30min/2h soak | 稳态无持续线性增长 |
| 控制可靠性 | input seq/ack | 授权会话内有序；关闭后 0 注入 |
| E2E skip | CI test report | 0 |

## 10. 数据迁移与回滚原则

- `allow_control` 缺字段迁移为 false；不得保留历史 fail-open 行为。
- 旧明文 token/identity 只允许一次性读入并写入安全存储，成功后安全删除；失败保持未登录/未绑定。
- 新旧 transport 并行期使用显式 protocol version 和 feature flag，不得静默猜协议。
- session schema 迁移需支持旧状态归一化；无法证明 active 的旧记录转 closed/unknown，不得继续算在线。
- 每张卡写回滚：代码回滚、配置开关、数据兼容、票据/会话失效方式。
- 安全红线修复不回滚到 fail-open；若新实现故障，回滚方式是禁用能力。

## 11. DeepSeek 每张任务卡模板

```markdown
# <CARD-ID> <标题>

## 目标与成熟度
一句话目标；before/after maturity。

## 需求与字段映射
逐字段：字段 <- docs/spec/...:行号；不改接口写 N/A。

## 能力链
UI -> domain -> server/signal -> transport -> OS/peer -> telemetry -> tests。

## 当前证据
文件:行号 + 可复现命令，不写猜测。

## 设计与不变量
状态、超时、错误、取消、幂等、安全、资源所有权、回滚。

## 只准改这些文件
精确白名单；生成物单列。

## 先写的失败测试
- TestName: 场景与断言

## 验收命令
定向测试、受影响栈全量、bash scripts/gate.sh。

## 完成证据
文件、字段映射、测试输出、gate、限制、回滚。
```

## 12. DeepSeek 执行提示词

可把下面内容连同单张任务卡交给 DeepSeek：

```text
严格遵守仓库根目录 AGENTS.md。只执行当前任务卡，不读取过期 PRD/历史 review 作为需求，不扩大文件白名单。
先输出字段映射和能力链；缺字段或 spec 冲突立即停止并指出，不猜。
先写能失败的测试，再写最小实现。禁止 UI 空壳、自动成功、固定 sleep 协调、硬编码 endpoint、吞错误、response:any、map[string]any、跳过测试和更新 baseline。
每 20-30 分钟检查一次 diff 是否偏离任务卡。完成前运行任务卡命令、受影响栈全量测试和 bash scripts/gate.sh。
回复必须贴改动文件、字段对应 spec 行号、每条验收断言的测试名和实际输出、gate 最终输出、未完成项与回滚。任一必跑项未执行时不得说“已完成”。
```

## 13. 推荐执行顺序

```text
ER-000 -> ER-001 -> ER-002 -> ER-003 -> ER-004
-> ER-010 -> ER-011 -> ER-012 -> ER-013 -> ER-014
-> ER-100 -> ER-101 -> ER-102 -> ER-110 -> ER-111 -> ER-112 -> ER-113
-> ER-200 -> ER-201 -> ER-202 -> ER-203 -> ER-204 -> ER-205
-> ER-400 -> ER-401 -> ER-402 -> ER-403 -> ER-410 -> ER-411
-> ER-500 -> ER-501 -> ER-502 -> ER-503 -> ER-504
-> ER-600 -> ER-601 -> ER-602 -> ER-603 -> ER-604 -> ER-610
-> ER-700 -> ER-701 -> ER-702 -> ER-710 -> ER-711
-> ER-800 -> ER-801 -> ER-810 -> ER-811 -> ER-812
-> ER-900 -> ER-901 -> ER-902 -> ER-903 -> ER-904
```

可并行的只有不共享契约/状态/所有权边界的卡。ER-500 的 ADR 可与 Phase 2/3 的 Go 控制面工作并行调研，但 ER-501 生产实现必须等 Phase 3 状态事件接口稳定。

## 14. 本轮文档变更的字段映射

本轮仅修改开发规范并新增整改总纲，没有新增、删除或改变任何 HTTP/协议请求响应字段，因此字段映射为 `N/A`，不运行 contract generator。
