# Ralph 推进计划：把「还差的」落成真功能

> 本文件是 Ralph 每一轮（无上下文的新 agent）的唯一事实来源。
> 每轮开始先读本文件 + `docs/impl/ralph-progress.md`，选**编号最小、未勾选**的任务做完并验证。

## 0. 环境事实（先读，别猜）

- 工作目录：`<repo>`
- 技术栈：`server/`（Go 1.26，net/http，内存 + PG 两套 store）、`client/`（Flutter 3.50，可编 Web/macOS，但**本机无 Xcode**）、`core/`（Rust workspace）、`web-admin/`（React 18 + Vite，纯 CSS）。
- **没有 git 仓库**，不允许 `git init`/`git commit`。改文件前先备份：
  `mkdir -p .ralph/backup/$(date +%s) && cp --parents <要改的文件> .ralph/backup/<ts>/`
- 端口：stack（网关+admin）8090、客户端 Web 8091、后台 dev 5174。这些可能正在运行，**不要**占用或杀掉；需要干净环境时用 `scripts/ralph-smoke.sh` 里的临时端口 8199/8191。
- 依赖：不新增第三方依赖（Go 标准库、React 现有依赖、Flutter 现有依赖）。**禁止** `npm install` 新包、`flutter pub add` 新包。
- 提交物只能是能在本机验证通过的代码 + 测试；不能留 `TODO` 占位实现。

### 工程约定（以仓库现状为准，别照搬 AGENTS.md 的缺失文件）
- 仓库根目录有 `AGENTS.md`，它引用的 `docs/spec/contract.yaml`、`scripts/gate.sh` **在本工作区不存在**。不要创建、不要假装跑过。
- 现有所有 handler 都是 `WriteJSON(... map[string]any{...})` + `WriteErr(...)`（`handler.go` / `adminhttp/handler.go`），**新接口保持同样的写法**，错误码统一走 `errcode`。
- 字段名 snake_case；业务里的验证码字段叫 `verify_code` 或用 `code` 之外的名字，避免与信封 `code` 混淆（本仓库信封是 `{code,name,...}`）。

## 1. 每轮工作协议（必须严格按顺序）

1. 读 `docs/impl/ralph-plan.md`、`docs/impl/ralph-progress.md`、`docs/impl/ui-refresh.md`。
2. 选出编号最小的未完成任务（`- [ ]`），记为 `TASK`。
3. 备份将改动的文件到 `.ralph/backup/<ts>/`。
4. 实现该任务的**全部**子项（后端 + 前端 + 测试），不要只做一半。
5. 跑该任务的「验收」命令，红了就修到绿。
6. 在本文件把该任务改成 `- [x]`，并把「完成证据」写在任务下面（测试名 + 关键输出一行）。
7. 在 `docs/impl/ralph-progress.md` 追加一段（时间、TASK、改动文件、验收输出摘要、下一步）。**追加，不要覆盖历史**。
8. 跑一次全量回归（限时，超时就缩小到受影响包）：
   - `cd server && gofmt -l . && go vet ./... && go test ./... -count=1`
   - `cd client && dart analyze lib && flutter test`
   - `cd web-admin && npm run typecheck && npm run build`
9. 输出结构化报告（最后一段，固定格式）：
   ```
   TASK: <id> <标题>
   STATUS: DONE | BLOCKED
   CHANGED: <文件列表>
   TESTS: <命令> -> <结果>
   NEXT: <下一个任务 id>
   NOTES: <一句风险或遗留>
   ```
   **只有所有 P0/P1 任务都勾选且回归全绿时**，报告 `RALPH COMPLETE`。

遇到无法在本机验证的硬阻塞（例如需要 Xcode、需要公网 STUN），把它标 `- [~] BLOCKED: <原因>` 并在报告写 `STATUS: BLOCKED`，然后**继续做下一个任务**，不要停。

## 2. 任务清单

### 现状校准（2026-11-20 22:20）
后端已由前一轮 Ralph 落地并验证（`scripts/ralph-smoke.sh` 全 PASS）：
- [x] P0-1 文件传输后端（`gateway/files.go` + `files_test.go`，store.FileMeta，冒烟 PASS）
- [x] P0-3 协助码后端（`gateway/assist.go` + `assist_test.go`，冒烟 assist create PASS）
- [x] P0-4 邮箱登录后端（`gateway/email.go` + `email_test.go`，冒烟 PASS）
- [x] P1-1 配置中心后端（`gateway/config.go`、`adminhttp /admin/v1/config`，冒烟 /v1/config PASS）
- [x] P1-3 STUN + /v1/net/reflect（`internal/netdiag` 有测试，冒烟 PASS）
未完成的是**全部前端接入**：P0-2 文件页、P0-3 协助码 UI、P0-4 邮箱 Tab、P1-1 后台配置页、P1-2 详情页、P1-4 诊断接配置。
下一轮直接做前端，不要重写后端；先跑一次冒烟确认后端仍绿。

### P0-1 文件传输后端（Go）（已完成，勿重做）
- [x] 新增 `/v1/files` 能力，绑定账号与设备凭证：
  - `POST /v1/files`：multipart 上传，单文件上限 32 MiB（超限 413），落盘到 `os.TempDir()/easyremote-files`，内存记录元数据（id、account_id、device_id、name、size、sha256、created_at）。
  - `GET /v1/files`：当前账号的文件列表（分页 20，按时间倒序）。
  - `GET /v1/files/{id}`：下载；**必须**校验请求方是该账号且持有有效的 `X-Device-Token` 或 Bearer 令牌之一（设计写进代码注释）。
  - `DELETE /v1/files/{id}`：删除（幂等）。
  - 所有写操作限流（复用 `ratelimit.Limiter`，键 `files:<accountID>`）。
- 存储：在 `server/internal/store` 增加 `FileMeta` 与接口方法（Memory + PG 两实现都要，PG 走 migrations）。
- 测试：`server/internal/gateway/files_test.go`（httptest）：上传→列出→下载字节一致、无令牌 401、超限 413、跨账号下载 403。
- 验收：`cd server && go test ./internal/gateway/... -run Files -count=1 && go test ./internal/store/... -count=1`

### P0-2 文件传输客户端页（Flutter，Web 可用）
- [x] `client/lib/pages/files_page.dart` + 侧边栏「文件传输」（桌面第 6 项 / 移动端底部导航第 6 项）：
  - 列表：`GET /v1/files`（名称/大小/时间/删除按钮），空态/错误态。
  - 上传优先用 Web 能力：`dart:html` 的 `FileUploadInputElement`（条件导入，参考 `lib/native_stub.dart` 的写法；桌面端走 `dart:io` 读文件）。
  - **降级路径（若条件导入 30 分钟内搞不定）**：提供「文件名 + 文本内容」表单，把 UTF-8 字节直接 `POST /v1/files`（multipart），并在 UI 注明「网页预览支持文本文件，桌面端支持本地文件」。降级也必须真调接口。
  - 下载：`GET /v1/files/{id}`，Web 下用 `ui_web.Url`/`window.open` 打开带票据的 URL（若需要 token，实现 `GET /v1/files/{id}/ticket` 一次性票据，仍走 WriteJSON 约定）。
  - 删除：`DELETE /v1/files/{id}`。
- 侧边栏加「文件传输」导航项（现在有 5 项，加第 6 项，移动端底部导航同时加）。
- 验收：`cd client && dart analyze lib && flutter build web --release --no-web-resources-cdn --dart-define=ER_GATEWAY=`
- 证据：用 Playwright 或 curl 走一遍真实接口（把命令写进 progress）。
  - 完成证据（2026-11-20 22:26）：`bash scripts/ralph-smoke.sh` → `PASS files upload/list name/ticket/download bytes/delete`（新增四条 curl 断言，票据下载故意不带 Authorization，模拟浏览器）。
  - 修掉一个真实后端缺陷：`downloadFile` 原先在取票据前就要求 Bearer，浏览器打开票据链接必然 401；改为 `downloadByTicket` 先原子核销票据再校验归属（`files_test.go:TestFilesDownloadTicketSingleUse` 已改为无请求头下载 + 单次 + 票据不可换 id）。
  - 平台差异如实标注：桌面端（dart:io）按路径读文件上传、下载落盘 ~/Downloads；Web 走「文件名 + 文本内容」上传 + 一次性票据链接复制。条件导入 `file_io.dart` / `file_stub.dart`。

### P0-3 协助码（临时授权）
- [x] 后端：
  - `POST /v1/assist/create`（被控设备凭证）：生成 6 位一次性协助码，TTL 10 分钟，记录 target_device_id、account_id。
  - `POST /v1/assist/redeem`（主控 Bearer + X-Device-Token）：用码换一个 `requested` 会话（等价 `/v1/connect/request` 的信任路径，无需 captcha），码只能成功兑换一次；过期/重复返回明确错误码。
  - 审计：兑换成功/失败都写 `assist.redeem`。
- 测试：`server/internal/gateway/assist_test.go`（生成→兑换→会话存在；二次兑换失败；过期失败；错误设备凭证 403）。
- [x] 客户端：连接页加「协助码」输入（6 位）+「兑换」按钮；成功后进入会话页。
  - 实现：连接页顶部「设备码 / 协助码」切换（`_ModeSwitch`），协助码 6 位数字输入 + 「兑换协助码」按钮 → `POST /v1/assist/redeem` → 直接进会话页；被控端入口在「我的设备 → 其他连接方式 → 生成协助码」（展示 6 位码 + 复制 + 10 分钟提示）。
  - 证据（2026-11-20 22:30）：`go test ./internal/gateway/... -run 'Assist|Email' -v` → 5 个 Assist 用例全 PASS；`dart analyze lib` → 0 error。
- 验收：`cd server && go test ./internal/gateway/... -run Assist -count=1` + `cd client && dart analyze lib`

### P0-4 邮箱登录（把 V1 占位变成真功能）
- [x] 后端：`POST /v1/auth/email/code`（dev 固定码 654321，60s 限流）、`POST /v1/auth/email/login`（邮箱不存在则创建账号），token 与手机号登录同构。
- 测试：`server/internal/gateway/email_test.go`：首次登录建号、二次登录同账号、错误码拒绝、限流触发。
- [x] 客户端登录页：手机号 / 邮箱 两个 Tab，邮箱 Tab 调用新接口；不再显示「邮箱登录 V1」占位。
  - 实现：`_AuthModeSwitch`（手机号 / 邮箱），邮箱 Tab 走 `POST /v1/auth/email/code` + `POST /v1/auth/email/login`，成功同样绑定设备进主框架；微信仍保留 V1 占位（后端没有）。
  - 证据：`go test ./internal/gateway/... -run 'Assist|Email' -v` → 3 个 Email 用例全 PASS；`dart analyze lib` → 0 error。
- 验收：`cd server && go test ./internal/gateway/... -run Email -count=1` + `cd client && dart analyze lib`

### P1-1 管理端配置中心（原型里有，缺后端）
- [x] 后端：`GET/PUT /admin/v1/config`（超级管理员，键值：`session.max_minutes`、`file.max_bytes`、`diag.auto_refresh_secs`、`client.force_update`），写操作进审计（before/after）。
- [x] 下发：`GET /v1/config`（客户端可读，返回白名单键）。
- 测试：`server/internal/adminhttp/config_test.go` + `gateway/config_test.go`。
- [x] 前端：`web-admin` 增加「配置中心」页（表单 + 保存 + 审计提示），导航加入。
  - 实现（2026-11-20）：`web-admin/src/App.tsx` 的 `ConfigCenter` 已接入 `PAGES` 导航（第 6 项「配置中心」）+ `{page === 'config' && <ConfigCenter/>}` 路由；字段渲染 int/bool、按 `min/max` 前置校验、非法项禁用保存、保存后绿条带「变更已写入审计（before / after）」、`撤销修改` 回滚草稿；`styles.css` 补 `.cfg-input(.bad)`。
  - 完成证据：`python3 scripts/verify-admin-ui.py` → `PASS config row rendered / save button enabled after edit / save banner mentions audit / audit row written -> config.update {"session.max_minutes":"720"}->{"session.max_minutes":"601"} / config persisted -> 601`；截图 `docs/impl/evidence/demo-screens/admin-config-center.png`。
- 验收：`cd server && go test ./internal/adminhttp/... ./internal/gateway/... -run Config -count=1` + `cd web-admin && npm run typecheck && npm run build`

### P1-2 管理端详情页
- [x] 设备详情：点设备行打开详情抽屉/页，展示设备码、归属账号、平台、在线、最后在线、最近 10 条会话（前端从 sessions() 过滤）。
- [x] 用户详情：点用户行打开详情，展示账号、状态、注册时间、该账号的设备（devices() 过滤）、最近会话。
  - 实现（2026-11-20）：`Devices` / `Users` 行加 `className="clickable"` + `onClick` 打开 `DeviceDetail` / `UserDetail`（`Modal` 形态）；行内「复制设备码」「封禁」按钮 `stopPropagation`，不会误开详情。数据全部由前端聚合，**没有新增任何后端接口**（`DeviceSessions` 用 `api.sessions()` 过滤 controller/target；`UserDetail` 用 `api.devices()+api.sessions()` 按 account_id / device id 过滤）。样式补 `.tbl tr.clickable`、`.modal .kv`、`.detail-h`。
  - 完成证据：`python3 scripts/verify-admin-ui.py` → `PASS copy button does not open detail / device detail opens on row click / device detail closes / ban button opens ban modal / user detail opens on row click / user detail shows account devices / no page errors`；截图 `docs/impl/evidence/demo-screens/admin-device-detail.png`、`admin-user-detail.png`。
- 验收：`cd web-admin && npm run typecheck && npm run build`；截图存 `docs/impl/evidence/demo-screens/admin-device-detail.png`。

### P1-3 STUN 反射服务（NAT 探测的地基）
- [x] Go：`server/internal/netdiag/stun.go` 实现最小 RFC 5389 Binding Request/Response（UDP），`cmd/stack` 可用 `-stun :3478` 起；
- [x] `GET /v1/net/reflect`（HTTP 侧回显客户端公网 IP:port 观察值，供 Web 诊断页显示"出口地址"）。
- 测试：`server/internal/netdiag/stun_test.go`（本地 UDP 回环：发 Binding Request，收到 XOR-MAPPED-ADDRESS 且等于本端地址）。
- [x] 客户端诊断页：「出口地址」用 `/v1/net/reflect` 真实数据；NAT 类型仍标「需桌面端 STUN」，不要伪造。
  - 实现：诊断页新增「出口地址（TCP）」指标卡 + 建议行，数据来自 `GET /v1/net/reflect`（`ReflectInfo.pretty`）；NAT 类型/打洞结果保持「待探测 / 需桌面端」。
  - 证据：`widget_test.dart` 断言 `出口地址（TCP）` 卡片存在；`flutter test` 18 passed；冒烟 `PASS net reflect -> 127.0.0.1`。
- 验收：`cd server && go test ./internal/netdiag/... -count=1`

### P1-4 诊断页接配置
- [x] 客户端设置页/诊断页读取 `/v1/config` 的 `diag.auto_refresh_secs` 与 `client.force_update`，真实生效（自动刷新间隔）。
  - 实现：`HomeShell` 登录后拉一次 `/v1/config` 得到 `AppConfig`，下发给诊断页/设置页/文件页：
    诊断页 `Timer.periodic(_autoSecs)` 自动测量（可开关，默认开，间隔 clamp 5–3600s）；设置页新增「运行参数」区块（会话上限 / 上传上限 / 诊断刷新间隔）并在 `client.force_update=true` 时顶部显示强制更新横幅；文件页用 `file.max_bytes` 做上传前置校验。
  - 证据：`cd client && dart analyze lib` → 0 error；`flutter test` → 18 passed。
- 验收：`cd client && dart analyze lib && flutter test`

### BLOCKED-E1 桌面端窗口
- [~] BLOCKED: 本机无 Xcode（`xcode-select -p` 指向 CommandLineTools），`flutter build macos` 不可用。**不要尝试安装 Xcode**。Web 预览是唯一可视化路径。

## 3. 完成定义

- P0-1..P0-4、P1-1..P1-4 全部 `- [x]`，且每项下面有「完成证据」。
- 全量回归全绿（server/client/web-admin 三条）。
- `docs/impl/ralph-progress.md` 有逐轮记录。
- 任一任务连续两轮无法推进 → 标 BLOCKED 并继续下一个，报告里说明。

## 4. P2 原型 1:1 对齐（2026-11-20 22:50 追加，优先级最高）

> 标准不是自由发挥，是 `prototype/index.html`。逐屏差距见 `docs/impl/prototype-parity.md`。
> 每屏完成标准：布局与原型同分区、真实接口数据、Playwright 截图 `docs/impl/evidence/demo-screens/proto-<id>.png`。

- [x] P2-0 差距表（`docs/impl/prototype-parity.md`）
- [x] P2-1 登录页回正（s01）：去掉「手机号/邮箱」大 Tab，恢复原型单表单；邮箱登录改为原型「邮箱登录」按钮可点 → 弹窗输入邮箱+验证码（真实 `/v1/auth/email/*`）；微信保持 V1 禁用
- [x] P2-2 文件传输页复刻（s07）：传输队列（真实上传任务，完成态 100%）+ 本机/远端双栏；本机栏用选择文件入口（Web），远端栏 = `/v1/files` 列表（下载/删除）；顶栏「暂停全部」「清除已完成」按原型
- [ ] P2-3 等待授权页复刻（s04）：设备信息头 + 连接信息表（本机设备码/连接方式/NAT/中继/加密/画质）+ 取消连接
- [ ] P2-4 会话中全屏 + 悬浮工具条（s05）：Web 占位同版式，工具条可断开/全屏/打开质量面板
- [ ] P2-5 质量详情面板（s06）：RTT/丢包（真实）+ 码率/帧率（标桌面端）+ 运行诊断 + 复制报告
- [ ] P2-6 本机设备页（s11）：大设备码 + 复制邀请链接 + 最近连接记录
- [ ] P2-7 授权弹窗（s12）：需要后端 `GET /v1/consents` + `POST /v1/consents/{id}`（accept/deny），客户端轮询弹窗
- [ ] P2-8 设置页重组（s10）：账号/安全/连接/画质/关于 五组
- [ ] P2-9 设备页顶栏补「添加设备」，导航按原型 5 项（自检移到设置-关于内）

完成 P2 后跑 `bash scripts/gate.sh` + 三端回归，并逐屏补截图。
