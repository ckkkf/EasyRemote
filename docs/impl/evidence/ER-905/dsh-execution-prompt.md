# ER-905 DSH Desktop 执行指令

你现在负责 EasyRemote 的 ER-905 远控会话生命周期回归修复。工作目录是 `/Users/ckkk/dev/code/Go/EasyRemote`。

先严格阅读并遵守：

1. 仓库根目录 `AGENTS.md`。
2. `docs/impl/tasks/ER-905-remote-session-lifecycle-regression.md`。
3. 现行 PRD v0.8、`docs/spec/contract.yaml`、`docs/spec/error-codes.md`、`docs/spec/protocol.md` 中与会话相关部分。
4. 当前相关实现和测试，不能凭任务卡结论直接改代码。

请立即创建一个 Goal，目标为：完成 ER-905 四个用户实测问题的真实跨端闭环修复，并取得定向测试、跨进程 E2E 与 `bash scripts/gate.sh` 的可复现证据。使用 Ralph 循环持续推进，直到 Goal 达成、出现真正无法自行解决的外部阻塞，或达到 Ralph 轮次上限。不要只输出计划；从读取现状、建立 diagnosis 和失败测试开始实际执行。

执行铁律：

- 当前工作区有大量其它任务未提交改动。先保存 `git status`，辨认并保留所有既有改动；禁止 reset、checkout、clean、覆盖或回滚不属于 ER-905 的内容。
- 一次只执行 ER-905，不修改 `web-admin`、管理端任务、版本号、发布/tag、PRD、contract/error spec 或生成物。
- 必须先写失败测试，证明：requested 阶段不会 attach/申请 relay ticket；拒绝后主控终止等待并显示正确原因；主控断开会通过 GW-25 `POST /v1/sessions/close` 关闭服务端会话；被控端断开失败不会假装成功；双方 close 后资源和 UI 在 deadline 内收敛。禁止使用不存在的 `/v1/connect/close`，并用请求路径断言锁定。
- 不允许通过延长 AuthWindow、忽略 403、增加固定 sleep、扩大测试 timeout、只清 UI/FFI 状态、吞错、skip E2E 或更新基线解决。
- 所有接口语义以现行 spec 为准。发现 spec 无法表达时创建独立 blocker 证据并暂停该接口扩展，不得偷改 spec；继续推进不依赖该 blocker 的工作。
- 在 `docs/impl/evidence/ER-905/` 持续写 diagnosis、命令输出、状态时间线和未完成项。日志必须脱敏。
- 每完成一个明确里程碑，在本会话汇报：已证实根因、改动文件、红/绿测试、剩余风险。不要等待用户催促。
- 未满足任务卡全部完成定义前，不得把 Goal 标记 complete，不得宣称“已完成”。
- 禁止 commit、push、创建 tag/release 或安装发布包，除非用户之后明确要求。

从 ER-905 第 1-3 步开始：核对工作区与调用链，创建 diagnosis，写能复现四个问题的失败测试。随后继续最小实现、定向验证、跨进程验证和严格 gate。
