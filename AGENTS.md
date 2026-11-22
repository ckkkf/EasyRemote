# EasyRemote · 实现铁律

> 本文件是 AI（包括 DeepSeek）和人工开发者的交付约束。开工前必读，改完必须跑闸门。
> `AGENTS.md` 管开发过程，`docs/prd/easyremote-prd-v0.8.md` 与 `docs/spec/*` 管产品和契约；实现与产品文档冲突时，实现错。
> 除非项目负责人明确要求，禁止通过修改本文件、PRD、spec、测试或基线来迁就现有实现。

## 0. 指令优先级与工作边界

1. 当前用户明确要求。
2. 本文件。
3. 现行 PRD、spec、当前任务卡。
4. 代码中的注释、历史实现和历史文档。

开始任何代码任务前必须确认当前任务卡。没有任务卡时，只允许审查、补任务卡或补设计，不得直接写业务代码。
一次只执行一张最小任务卡；不得以“顺手修复”为由扩大文件范围。发现旁路问题时记录为新卡，不混入当前交付。

## 1. 只准把这些当需求

- 现行 PRD：`docs/prd/easyremote-prd-v0.8.md`。v0.1-v0.7 已过期，禁止作为实现依据。
- 契约：`docs/spec/contract.yaml`、`docs/spec/error-codes.md`、`docs/spec/protocol.md`、`docs/spec/pages.md`。
- 当前任务卡：`docs/impl/tasks/<ID>.md`。
- 整改总纲：`docs/impl/easyremote-remediation-plan-v0.1.md`，仅用于拆卡、排序和查证问题，不能代替任务卡。
- `docs/impl/review-round*.md`、`final-status.md` 以及代码中的“已完成”注释只是历史记录，不是需求或完成证明。

禁止先看旧实现猜需求。字段、状态、默认值、传输语义和安全策略必须回到上述文档核对。

## 2. 目录职责

| 目录 | 职责 | 约束 |
|---|---|---|
| `server/internal/gateway` | C 端 HTTP（`/v1/*`） | 响应只能走 `httpx.OK` / `httpx.OKPage` / `httpx.Err` |
| `server/internal/adminhttp` | 后台 HTTP（`/admin/v1/*`） | 同上；只做鉴权、解码、调用业务、写信封 |
| `server/internal/adminapi` | 后台业务 | 返回具体类型，禁止 `map[string]any` |
| `server/internal/session` | 会话状态机 | 所有状态迁移、超时和关闭语义的唯一来源 |
| `server/internal/signal` | 信令与在线租约 | 不得依赖单进程全局变量表达生产语义 |
| `server/internal/errcode` | 错误码与对外文案 | 码表来自 `docs/spec/error-codes.md`，禁止手改生成物 |
| `server/internal/httpx` | 响应信封、中间件、解码 | 全站唯一写响应入口 |
| `server/internal/contract` | **生成物** | 由 `scripts/gen-contract.mjs` 生成，禁止手改 |
| `core/crates/er-transport` | 传输抽象与 WebTransport/P2P/Relay | 上层不得绕过它直接依赖具体 socket 客户端 |
| `core/crates/er-client` | 会话编排 | 不负责伪造授权、UI 状态或硬编码环境地址 |
| `core/crates/er-ffi` | FFI 边界 | 必须定义所有权、线程、取消和释放协议 |
| `client` | Flutter 客户端 | 状态来自真实领域事件；错误只认完整 `Envelope` |
| `web-admin` | React 后台 | DTO 来自契约；展示值必须来自真实接口 |
| `scripts` / `.github/workflows` | 本地与 CI 闸门 | 本地严格闸门和 CI 必须覆盖同一组关键检查 |

依赖方向必须单向：UI -> 应用服务/FFI -> 领域状态机 -> 传输/存储。禁止 UI 直接拼协议、传输层反向控制 UI、上层直接访问具体 UDP/TCP 实现。

## 3. “已实现”的唯一判定

### 3.1 端到端闭环

一个功能只有同时具备以下链路才可标记为“已实现”或在 UI 中默认可用：

1. 用户入口与可理解的进行中、成功、失败、重试、取消状态。
2. 真实领域状态和合法状态迁移，不用布尔值拼凑复杂生命周期。
3. 真实副作用：网络、系统 API、持久化或对端行为确实发生。
4. 错误能沿原链路返回，不能吞掉后继续显示成功。
5. 取消、超时、断线、退出和资源释放闭环。
6. 指标、日志和审计来自真实执行点，而不是 HTTP 健康检查或 UI 计时器代替。
7. 至少一个跨边界测试证明入口到副作用，另有失败路径测试。

缺任一项时只能是 `experimental` / `disabled`，必须由能力开关隐藏或明确禁用；禁止做成可点击的正常功能。

### 3.2 禁止虚假实现

以下做法一律打回：

- 只有页面、按钮、开关或成功提示，没有领域逻辑和真实副作用。
- 用固定文案、固定状态、固定指标、随机数或 `/healthz` 延迟冒充会话指标。
- `catch (_) {}`、`_ = err`、空回调后仍显示成功；允许忽略的错误必须写明理由并有计数器。
- 用 `sleep`、固定延迟或 UI 轮询协调握手、授权和状态迁移。
- 在生产路径自动接受授权、自动打开安全开关或遇错降级为允许。
- 用 mock/fake/in-memory 实现接生产入口；测试替身只能存在于测试或显式开发构建。
- 注释声称“真实”“立即”“实时”“已完成”，但没有测试和可观测证据。
- 为让测试通过而删断言、扩大超时、跳过测试、更新快照/基线或修改 spec。

### 3.3 功能成熟度台账

新增或重构功能必须在任务卡中列出能力链：`UI -> domain -> server/signal -> transport -> OS/peer -> telemetry -> tests`，逐项给出文件和测试名。
任务卡必须声明成熟度：`disabled`、`experimental`、`beta`、`stable`。只有端到端验收与回滚均完成后才能升为 `stable`。

## 4. 远控安全红线

1. `allow_control` 默认必须为 `false`；历史数据缺字段也按 `false` 迁移，禁止 fail-open。
2. 跨账号连接必须由被控端本机显式接受。主机核心、后台、主控端和远程输入均不得代替用户接受。
3. 被控端授权弹窗必须展示控制者信息、30 秒倒计时、接受、拒绝；超时等价于拒绝。
4. 会话中被控端必须常驻本地控制条并可一键断开；远端不能隐藏、关闭或修改本地安全设置。
5. “已授权”不等于“已连接”。只有双向认证传输建立并收到数据面就绪确认，服务端才可进入 `active`。
6. 任一端断开、被控端收回权限或后台强制中断，都必须向双方传播关闭事件、停止采集/输入、撤销票据并释放资源。
7. 断开后的旧票据、旧授权窗口和旧输入事件必须失效；重放测试必须覆盖。
8. 隐私屏语义是“本机物理显示不可见、远端采集继续”，不是把采集帧涂黑。无法满足时必须禁用入口。
9. 固定密码、临时验证码、设备密钥、会话令牌必须使用 CSPRNG；禁止时间戳、PID、LCG 或可预测随机数。
10. 账号令牌、设备私钥、长期密码不得以明文 JSON/localStorage 作为桌面稳定方案；使用系统 Keychain/Credential Manager/libsecret。Web 方案必须有明确威胁模型。

## 5. 信令、在线状态与会话状态机

- 生产信令必须支持多进程/多实例；网关不能依赖进程内全局 Hub。跨实例通知使用共享 Bus，并有投递确认或可观察失败。
- 创建请求前必须用在线租约判断目标；通知器缺失、目标离线或投递失败时请求必须失败，不能返回“等待授权”。
- 在线状态来自信令连接租约：注册写入、心跳续租、断开撤销、实例崩溃靠 TTL 过期。绑定设备不等于在线。
- `requested -> authorized -> active -> closed` 以及失败分支只能由 `server/internal/session` 迁移。HTTP handler、UI 和管理后台不得自行解释状态。
- 每个有时限的状态必须有主动过期机制，不能只在下次操作时顺便清理。
- 幂等键必须包含足够上下文和有效期；已过期/已关闭会话不得被 `Request` 复用。
- 状态变更必须产生领域事件和审计，事件携带 `session_id`、from/to、old/new state、reason、trace_id、时间。
- 后台计数、在线列表和质量统计必须读取同一权威状态，不得从旧布尔字段或前端缓存推导。

## 6. 传输、媒体和输入

1. `docs/spec/protocol.md` 是通道语义唯一来源：媒体走可丢 datagram，控制/心跳走可靠有序流，文件/剪贴板走可靠可流控双向流。
2. `er-client` 必须通过 `er-transport` 的接口工作。禁止在会话编排层直接构造 `UdpSocket`、`RelayClient` 或私有分片协议。
3. 必须实现并测试 STUN/ICE 候选、P2P 打洞、Relay 兜底和链路选择；只实现 Relay 不能宣称符合现行架构。
4. 握手和重连由显式状态机、事件、deadline 与 cancellation 驱动；禁止靠两端对齐固定 `sleep`。
5. 输入事件必须可靠、有序、有权限门控；视频拥塞不得阻塞本地断开、心跳和输入。
6. 视频队列必须有上限并采用“保最新帧”背压；禁止无限排队造成累积延迟。
7. Flutter 画面必须走原生纹理/零拷贝路径。禁止每帧 `malloc -> RGBA 拷贝 -> Dart decodeImageFromPixels` 作为正式方案。
8. 编解码器必须报告实际采集、编码、发送、接收、解码、呈现时间；FPS 是呈现帧数，不是轮询次数。
9. 资源生命周期必须可证明：socket、线程、任务、codec、texture、native string、buffer 在成功/失败/取消路径均释放。
10. 性能目标按 PRD 的测试场景验证；达不到时显示实际能力并阻止成熟度升级，禁止用降到 12 FPS 后仍标“1080p60”。

## 7. 配置与环境

- Gateway、signal、STUN、Relay、WebTransport endpoint 统一来自服务端配置/运行时配置对象；业务页面和核心库禁止硬编码地址、端口和协议。
- `127.0.0.1`、开发账号、开发 secret、跳过 MFA 只可出现在测试、example 或显式 `development` 配置；生产构建发现即失败。
- 生产默认必须 TLS/WSS/HTTP3，证书校验不得静默关闭。开发明文必须显式 opt-in。
- 配置加载必须校验完整性和相互一致性；端口不一致、缺 notifier、缺 Redis/Bus 等必须启动失败，不得半工作启动。
- 所有重试必须有 deadline、指数退避、抖动和取消；用户操作不得在 UI isolate/主线程做阻塞网络或 FFI 工作。

## 8. HTTP、契约与错误

1. 响应只能由 `httpx.OK` / `httpx.OKPage` / `httpx.Err` 产出。成功体固定 `{code:0,name:"OK",data,trace_id}`；错误体固定 `{code,name,message,retryable,trace_id}`。
2. 禁止 `json.NewEncoder` 直接写响应、`http.Error`、`w.Write([]byte(...))`、裸 `map[string]any` 响应。
3. 改接口必须先改 `docs/spec/contract.yaml`，运行 `node scripts/gen-contract.mjs`，再写实现。顺序反了就是漂移。
4. 契约不得使用 `response: any` 作为完成态。请求、响应、分页、枚举、错误集必须有具体 schema。
5. 每条路由必须验证 HTTP method、path、auth、request、response 和 errors；只比较 path 的检查不算契约测试。
6. Handler 必须拒绝错误 method，并返回契约错误信封；禁止 `HandleFunc` 导致任意 method 落入同一业务逻辑。
7. 错误码只能从 `errcode` 选。新增码先改 `docs/spec/error-codes.md` 再运行 `node scripts/gen-errors.mjs`。
8. 字段命名一律 `snake_case`；`code` 键只归信封，业务验证码叫 `verify_code`。
9. Flutter/React 必须保留 `code/name/message/retryable/trace_id`，UI 的错误详情需能复制 `trace_id`。禁止只保留字符串 message。
10. 生成 DTO/客户端是唯一接口类型来源；禁止 Go、Dart、TypeScript 三端分别手写同一结构。
11. 不许改 `docs/spec/*` 去迁就实现。spec 与实现不一致时，实现错；确需改需求必须由负责人先批准并单独提交 spec 变更。

## 9. 代码质量规范

### 9.1 通用

- 先读调用链和测试，再改代码；禁止凭文件名猜行为或整模块重写。
- 一个概念一个权威来源。状态、配置、时间、在线性、指标和权限不得多处各算一遍。
- 依赖注入必须显式；必需依赖不能是可空字段后静默跳过。
- 禁止魔法数字、硬编码状态字符串和散落超时。统一命名常量并标注来源。
- 错误必须带上下文但不得泄露 token、密码、设备密钥、验证码和敏感画面信息。
- 日志必须结构化并包含 `trace_id` / `session_id`（适用时）；禁止记录 secret 和完整凭证。
- 公共 API、状态机和复杂并发块需有解释“为什么”的短注释；禁止用注释掩盖临时方案。
- 新抽象必须删除实际重复或建立明确边界；禁止为包装一行调用制造层级。

### 9.2 Go

- Handler 只做协议适配；业务放 service，持久化放 store。
- 所有 I/O 接收 `context.Context` 并尊重取消；goroutine 必须有 owner、停止信号和等待策略。
- 禁止忽略生产路径错误。事务中状态变更、审计和 outbox 必须保证一致性。
- 新共享状态必须通过 `go test -race`；分页必须由存储层实现，不得取固定上限后前端切片。

### 9.3 Rust

- `unsafe` 仅限 FFI/平台边界，必须写安全不变量和对应测试。
- 禁止在异步任务/UI 调用链使用阻塞 sleep 或无上限 blocking I/O；需要阻塞时隔离到专用线程并可取消。
- 每个线程/task 都必须有 shutdown 与 join；跨线程状态优先消息传递，锁要短且不得包住 I/O/解码。
- FFI 字符串、指针、buffer 的分配者和释放者必须成对；Dart 传入的 native allocation 必须 `calloc.free`。
- `cargo fmt`、`cargo clippy --all-targets -- -D warnings`、`cargo test --workspace` 为完成条件。

### 9.4 Dart/Flutter

- 网络、握手、解码和阻塞 FFI 不得在 UI isolate 同步执行。
- Widget 不得持有会话真相；使用可测试的 controller/state machine，生命周期内取消 Timer/Stream/Future/native handle。
- 禁止空 catch。用户可恢复错误要展示动作；不可恢复错误要进入明确失败态。
- 平台能力不可用时禁用入口并说明原因，不展示可操作的空壳页面。
- `flutter analyze` 必须零 error、零 warning；新增代码零 info。存量 lint 必须有单独收敛卡，不能继续增加。

### 9.5 TypeScript/React

- 开启并保持 strict；禁止 `any`、非空断言掩盖数据问题和重复手写契约类型。
- 服务端分页、筛选、排序必须真的传给 API；禁止只对已截断结果做客户端分页。
- 登出必须撤销服务端 session；角色、账号和权限来自登录响应，不得硬编码。
- token 存储方案必须有威胁模型；不得把 `localStorage` 当作无风险默认。
- 每个写操作必须处理 pending/success/error、重复提交、刷新失败和权限拒绝。

## 10. UI 与视觉不变量

1. 同一视觉属性只能有一个来源。底色、边框、圆角、阴影、悬停/按下反馈要么交给框架，要么完全自绘，禁止叠两层。
2. 过渡只允许在同语义的合法状态之间插值。路径中每一帧都必须是可能出现的合法状态。
3. 淡出只动 alpha/opacity。透明目标必须与背景同 RGB、alpha=0；禁止 `Colors.transparent` 导致 RGB 向黑色插值。
4. 禁止把尺寸、偏移、圆角、亮度 tween 到 0/负值来表达“没有”；先建模合法的“无”状态。
5. UI 修复默认方向是减代码并优先原生组件。净增行数为正时，交付说明必须解释为何不能删除自定义实现。
6. 视觉修复必须提供像素采样、几何测量或组件属性断言之一；“看起来好了”不算验证。
7. 测试守不变量，不守偶然实现细节。页面文案不得承诺后端/传输尚未提供的实时性、立即性或安全性。

## 11. 测试与闸门

### 11.1 最低测试层级

- 状态机/算法：单元测试，覆盖合法迁移、非法迁移、超时、幂等、取消和重放。
- HTTP：契约测试覆盖 method/path/auth/request/response/errors 与完整信封。
- 信令/传输：真实多进程或容器集成测试，覆盖跨实例通知、心跳续租、离线、P2P、Relay fallback、丢包和重连。
- FFI：所有权与失败释放测试；至少做一次长时资源增长检查。
- UI：controller/widget 测试覆盖真实状态映射；关键流程做桌面 E2E，不能只断言控件存在。
- 性能：按 PRD 固定场景保存原始数据、环境、版本和阈值，不得只贴截图或主观描述。
- 安全：授权绕过、旧票据重放、跨设备访问、错误 method、token 泄露、CSRF/Origin、速率限制和日志脱敏。

### 11.2 禁止“跳过即通过”

- 因缺环境变量、服务、工具链或平台而 skip 的测试不计入完成。
- E2E 所需服务必须由脚本/容器自动启动；测试失败后自动收尾。
- 本地无法运行某项时必须明确标记“未完成”，不能以 CI 可能会跑为依据宣称完成。
- flaky test 必须先修根因；禁止靠重跑、增大 sleep 或吞失败通过。

### 11.3 必跑矩阵

`bash scripts/gate.sh` 必须逐步收敛为以下命令的统一入口。在闸门尚未覆盖前，任务必须手动执行受影响部分：

```bash
node scripts/gen-errors.mjs --check
node scripts/gen-contract.mjs --check
node scripts/check-routes.mjs --strict

cd server && gofmt -l . && go vet ./... && go test -race ./...
cd core && cargo fmt --all -- --check
cd core && cargo clippy --workspace --all-targets -- -D warnings
cd core && cargo test --workspace
cd client && dart format --output=none --set-exit-if-changed lib test
cd client && flutter analyze
cd client && flutter test
cd web-admin && npm ci && npx tsc --noEmit && npm test -- --run && npm run build

bash scripts/gate.sh
```

若项目尚无某条命令对应的测试框架，先创建闸门任务卡，不得删除该要求。

## 12. 标准工作流

```text
读任务卡
-> 输出字段映射表（字段 <- spec 行号；不改接口则逐项写 N/A）
-> 列能力链与当前缺口
-> 必要时先写 ADR/Spike，并得到可执行结论
-> 改 contract/error spec（仅接口确需变化时）
-> 运行生成器
-> 写失败测试
-> 写最小实现
-> 跑定向测试
-> 跑受影响技术栈全量测试
-> 跑 bash scripts/gate.sh
-> 检查 diff、硬编码、资源释放、安全与回滚
-> 贴证据
```

字段映射缺一行就停下来问，不要猜。架构级任务没有 ADR/Spike 结论时不得直接大改。任务卡外发现的问题只登记证据和建议卡号。

## 13. 完成定义与回复格式

只有同时满足以下条件才可说“已完成”：

- 任务卡每条验收断言都有唯一、可搜索的测试名且全绿。
- 功能链从入口到真实副作用闭环，失败、超时、取消、断开和回滚均验证。
- `bash scripts/gate.sh` 严格模式全绿，且受影响栈的必跑矩阵全绿。
- 没有新增 `any`、硬编码 endpoint/secret、空 catch、忽略错误、固定 sleep 协调、跳过测试或基线债务。
- 文档、契约、生成物、实现和 UI 文案一致。
- 安全与性能要求有可复现证据；未达到的能力保持禁用。

交付回复必须按以下顺序给出：

1. 改动文件及每个文件的目的。
2. 字段映射表：每个请求/响应字段对应 spec 行号；未改接口明确写 `N/A`。
3. 验收断言 -> 测试名 -> 结果。
4. 实际执行的命令与关键完整输出（至少包含 gate 最终行）。
5. 未运行项、已知限制、风险和回滚方式。存在未运行必跑项时不得写“已完成”。

## 14. 常用命令

```bash
bash scripts/gate.sh                    # 严格闸门；任何一步红都不算完成
bash scripts/gate.sh --report           # 仅审查存量债务，不可作为完成证明
bash scripts/gate.sh --update-baseline  # 负责人批准后才可执行，并需单独说明
node scripts/gen-contract.mjs
node scripts/gen-contract.mjs --check
node scripts/gen-errors.mjs --check
```

禁止伪造命令输出、测试结果、截图、性能数字或文件行号。
