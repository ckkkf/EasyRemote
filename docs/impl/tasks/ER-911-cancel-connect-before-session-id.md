# ER-911 · GW-06 未返回会话 ID 时的远控取消（待批准协议变更）

状态：设计 / 未实现。成熟度：现有取消不升级。批准前不变更产品文档、契约、迁移或公开接口。本卡限设备码连接；协助码兑换另有单次消费语义，不复用本卡的结论。

## 现状与不可兼容之处

- `client/lib/pages/connect_page.dart:145–185`：取消立即清理本地等待状态；若 GW-06 迟到则丢弃结果。被控端申请可能已经送达。
- `server/internal/gateway/handler.go:399–432`：服务端可先登记、通知，再返回 `session_id`；`server/internal/session/session.go:103–150` 的设备对防重允许重试得到同一个 ID。GW-06 的 `trusted` 不表示创建者或取消权。
- GW-25 仅接受会话 ID 和既有设备凭证（`docs/spec/contract.yaml:846–852`）；在会话 ID 未知的窗口，现有客户端无法可靠定位本次尝试。中止 HTTP 请求也不能撤回已提交的事务。按设备对关闭或无条件关闭一条迟到响应中的 ID，都会误伤正在使用该会话的另一次尝试。
- 产品期望：`docs/spec/pages.md:79` 的取消操作必须真实撤下申请；跨账号必须本机授权，不能以修复取消为由削弱（PRD §5.1.2）。

## 拟议最小契约（不是已上线接口）

- GW-06 增加可选、由发起端安全生成的 `attempt_id`；只对使用新协议的设备提供“撤回在途申请”承诺。旧请求字段保持兼容。
- 新增以 `attempt_id` 定位的取消命令。服务端只允许发起设备用原设备凭证取消；取消早于发起时记录有时限的取消标记。建议响应仅给出尝试标识和结果状态，不能回送凭证。命名、具体路由、长度、状态枚举、错误集与 TTL 要在契约评审中确定。
- 字段来源：GW-06 `ConnectResult` 的 `session_id/state/trusted` 仍对应 `contract.yaml:367–370,748–753`；GW-08/GW-25 的会话状态字段保持 `375–378,761–766,846–852`。新增字段无现行 spec 行号，须在负责人批准后先落定契约并生成两端 DTO。

## 执行顺序与并发不变量

1. 建连和取消以经验证的主控设备及尝试 ID 为键，在同一数据库事务中序列化结果。已取消的尝试不重新落请求或发通知；同 ID 幂等。旧尝试永远不能关闭由另一尝试创建的会话。若同设备对已有非终态会话，新尝试可收到冲突或走明确的非拥有者恢复流程，不能顺手转移取消权；终端已接管的会话用 GW-25 明确结束。
2. 取消已登记请求时走 session 状态机：与授权/就绪竞争，服务端按持久化的终态收敛。已结束的操作幂等；授权、撤回、审计和待投递事件必须有一致性保证。限流、TTL 和清理要避免堆积取消标记；避免使用设备码或任意 ID 批量撤回。
3. 先接通生产信令的关闭与恢复链路，再开放新客户端能力。被控端按会话 ID 和顺序处理通知；收到关闭后不得被晚到的授权申请重新弹窗。客户端立即退出本地等待，但未收到服务端确认时展示“撤回未确认”，而非“已撤回”。
4. 能力链：Flutter → GatewayClient/生成 DTO → GW-06 与取消命令 → session / PG 事务 → Redis 信令或补偿投递 → 被控端弹窗/输入释放 → 会话事件和回归。涉及数据库和跨实例信令，不用仅 UI 测试作为完成证据。

## 计划验收

- `TestCancelBeforeConnectRequestPreventsConsentDelivery`：取消先到，后到的发起不能落会话或发消息。
- `TestCancelAfterCommitBeforeHTTPResponseWithdrawsConsent`：GW-06 落库并进入通知后延迟 HTTP 响应；取消要使两端终态收敛。
- `TestCancelledAttemptCannotCloseReplacementOrForeignSession`：取消后立即发同设备的新尝试，旧 cancel/旧响应重放、不同设备冒充都不影响新会话。
- `TestDelayedConsentCannotReopenCancelledAttempt`：双网关 + 真实 PG/Redis，乱序和重投递不重新弹窗。
- `TestConnectPageShowsCancellationUntilServerConfirms`：本地可离开，但断网时不会假报已撤回。最后跑 Go race、Flutter 与严格 gate，再做两台桌面端验收。

回滚设计：新客户端能力默认关闭直到服务端支持；先停新协议入口并保留在途取消标记至 TTL，再撤回服务端部署。尚无代码和 schema 可回滚。
