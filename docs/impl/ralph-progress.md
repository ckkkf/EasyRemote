# Ralph 进度日志

> 只追加，不覆盖。每轮一段：时间 · TASK · 改动 · 验证 · 下一步。

## Round 0（人工初始化，2026-11-20 22:10）
- 建立计划：`docs/impl/ralph-plan.md`（P0-1..P0-4、P1-1..P1-4、BLOCKED-E1）
- 现状：客户端/管理端 UI 已按原型重做（见 `docs/impl/ui-refresh.md`），后端仅有 auth/devices/connect/relay/admin 基础接口。
- 下一步：从 P0-1 开始。
## Round 1（2026-11-20 22:08–22:16，Ralph 第 1 轮）
- TASK：P0-1 文件传输后端 / P0-3 协助码后端 / P0-4 邮箱登录后端 / P1-1 配置后端 / P1-3 STUN+reflect
- 改动：`server/internal/gateway/{files,assist,email,config}.go` 及测试、`server/internal/store/store.go`（FileMeta/AssistCode）、`server/internal/netdiag/`、`server/cmd/stack/main.go`（-stun）、`server/internal/adminhttp/handler.go`（/admin/v1/config）
- 验证：`go build ./...` OK；`go test ./internal/gateway/... ./internal/store/...` ok；`bash scripts/ralph-smoke.sh` → files list / assist create / email code / client config / net reflect 全 PASS
- 备注：轮次被 10 分钟调用上限截断，计划勾选与日志由 lead 补齐；前端未开工
- 下一步：P0-2 文件页（客户端）

## Round 2（2026-11-20 22:22 起，Ralph 第 2 轮 · 前端接入）
### P0-2 文件传输客户端页 ✅
- 改动：
  - 新增 `client/lib/pages/files_page.dart`（列表/空态/错误态、上传对话框、下载、删除确认、相对时间、大小格式化）
  - 新增 `client/lib/file_io.dart`（dart:io 读文件 + 落盘 ~/Downloads）与 `client/lib/file_stub.dart`（Web 替身），页面用条件导入 `if (dart.library.js_interop)`
  - `client/lib/shell.dart`：导航加第 6 项「文件传输」，并在 shell 内拉一次 `/v1/config`（`_config`）下发给文件页做上传上限前置校验
  - `client/lib/api.dart`：新增 `downloadFile(id)`（带凭证取字节，桌面落盘用）
  - `server/internal/gateway/files.go`：修票据下载鉴权（`downloadByTicket`，浏览器无请求头可用）、`files_test.go` 同步加严（无头下载/单次/票据不可换 id）
  - `scripts/ralph-smoke.sh`：healthz 断言改成信封形状；新增 upload→list→ticket→download→delete 五条真实 curl 断言
- 验收：
  - `cd client && dart analyze lib` → 0 error / 0 warning（仅既有 info 级 lint）
  - `flutter build web --release --no-web-resources-cdn --dart-define=ER_GATEWAY=http://127.0.0.1:8090` → `✓ Built build/web`
  - `bash scripts/ralph-smoke.sh` → 13 行全 PASS（含 `PASS files download bytes`）
  - `cd server && gofmt -l . && go vet ./... && go test ./internal/gateway/... -count=1` → ok
- 下一步：P0-3 协助码 UI（连接页）

### P0-3 协助码 UI / P0-4 邮箱 Tab / P1-3 出口地址 / P1-4 诊断接配置 ✅（客户端）
- 改动：
  - `client/lib/pages/connect_page.dart`：设备码 / 协助码 切换（`_ModeSwitch`）+ 6 位协助码输入 + 「兑换协助码」→ `POST /v1/assist/redeem` → 直接进会话页
  - `client/lib/pages/device_list_page.dart`：「其他连接方式」新增「生成协助码」→ `POST /v1/assist/create`，弹窗展示 6 位码 + 复制 + 10 分钟/一次性提示
  - `client/lib/pages/login_page.dart`：手机号 / 邮箱 两个 Tab（`_AuthModeSwitch`），邮箱走 `/v1/auth/email/code` + `/v1/auth/email/login`，删掉「邮箱登录 V1」占位；微信仍标 V1（后端无）
  - `client/lib/pages/diagnostics_page.dart`：出口地址卡（`/v1/net/reflect`）、`diag.auto_refresh_secs` 驱动的自动刷新（可开关）、建议行
  - `client/lib/pages/settings_page.dart`：`config` 入参、「运行参数」区块、`client.force_update` 强制更新横幅；`_Section` 补 `Material(transparency)`（修 Flutter「ListTile 背景遮 ink」断言）
  - `client/lib/shell.dart`：登录后拉 `/v1/config` 下发三页、设置本机设备码（协助码生成需要）
  - `client/test/widget_test.dart`：修长期编译不过的陈旧断言（`DiagnosticsPage(device:)` → `client:`、设备页补 `onConnect`、设置页滚动到「本机」）
- 验收：
  - `cd server && go test ./internal/gateway/... -run 'Assist|Email' -count=1 -v` → 8 个用例全 PASS
  - `cd client && dart analyze lib` → 0 error / 0 warning（36 条 info 级 lint，与既有代码同性质）
  - `cd client && flutter test` → `All tests passed!`（18 passed，含诊断页出口地址卡断言）
- 顺手修的坑（不是本轮任务但会挡验收）：`scripts/gen-contract.mjs` 把契约字段 `default` 直接当 Dart 标识符，生成物 `client/lib/contract.dart` 编译不过；已加 `dartField()` 把 Dart 保留字映射为 `defaultValue`（JSON 键不变），重跑 `node scripts/gen-contract.mjs`。
- 下一步：P1-1 后台配置页（web-admin）


## Round 3（2026-11-20 22:35–22:52，Ralph 第 3 轮 · 管理端接线收尾）
- TASK：P1-1 配置中心前端接入 · P1-2 管理端详情页（另带 gate 转绿）
- 起点状态：`bash scripts/gate.sh` → `GATE: FAIL`，唯一红灯是 `web-admin` tsc 的三个 TS6133（`ConfigCenter` / `DeviceDetail` / `UserDetail` 已写好但没接入）。
- 改动文件：
  - `web-admin/src/App.tsx`
    - 配置中心：`PAGES` 里的「配置中心」导航 + 新增 `{page === 'config' && <ConfigCenter notice={notice} />}` 路由（此前只有导航项、没有渲染分支）。
    - 设备详情：`Devices` 增加 `detail` state，表格行 `className="clickable"` + `onClick={() => setDetail(d)}`，行尾渲染 `<DeviceDetail device={detail} onClose=.../>`；「复制设备码」按钮补 `e.stopPropagation()`。
    - 用户详情：`Users` 增加 `detail` state，行 `className="clickable"` + `onClick={() => setDetail(u)}`，渲染 `<UserDetail user={detail} onClose=.../>`；「封禁」按钮补 `e.stopPropagation()`，封禁弹窗与详情弹窗互不干扰。
    - 数据仍由前端聚合：`DeviceSessions` 用 `api.sessions()` 过滤 controller/target；`UserDetail` 用 `api.devices() + api.sessions()` 按 account_id / device id 过滤。**server/ 与 client/ 一行未改，没有新增接口。**
  - `web-admin/src/styles.css`：补 `.tbl tr.clickable`（指针 + 悬浮高亮）、`.modal .kv`（详情键值两列）、`.detail-h`（详情小标题）、`.cfg-input` / `.cfg-input.bad`（配置项输入，非法值红框）。此前这些 class 全无样式。
  - `scripts/verify-admin-ui.py`（新增）：管理端 UI 验收脚本（python playwright + 本机 Chrome，不往 web-admin 加依赖）。自建临时 stack :8199 + 静态服务 :5292，拦截 `/admin/v1/*` 转发到临时 stack，**不碰 8090/8091/5174 上正在跑的服务**。
  - 截图（新增）：`docs/impl/evidence/demo-screens/admin-config-center.png`、`admin-device-detail.png`、`admin-user-detail.png`。
  - 文档：`docs/impl/ralph-plan.md` 勾选 P1-1 前端项与 P1-2 两项并写完成证据。
- 验收/命令输出摘要：
  - `bash scripts/gate.sh` → `GATE: PASS`（1/8 gofmt 空、2/8 go vet+test ok、3/8 web-admin tsc 无输出、4/8 三端契约与错误码一致、5/8 路由 30 条一致、6/8 无新增违规、7/8 信封契约 ok）。
  - `cd web-admin && npm run typecheck` → 无输出（tsc --noEmit 通过）。
  - `cd web-admin && npm run build` → `✓ 32 modules transformed / dist/assets/index-*.js 171.95 kB / ✓ built in 341ms`。
  - `python3 scripts/verify-admin-ui.py` → 12 项全 `PASS`、末行 `VERIFY OK`：
    `PASS config row rendered`、`PASS save button enabled after edit`、`PASS save banner mentions audit -> 已保存 1 项，变更已写入审计（before / after）`、`PASS audit row written -> config.update {"session.max_minutes":"720"}->{"session.max_minutes":"601"}`、`PASS config persisted -> 601`、`PASS copy button does not open detail`、`PASS device detail opens on row click`、`PASS device detail closes`、`PASS ban button opens ban modal -> 封禁 acc-…`、`PASS user detail opens on row click`、`PASS user detail shows account devices`、`PASS no page errors`。
  - 截图人工核对：配置中心 4 个配置项（`session.max_minutes` / `file.max_bytes` / `diag.auto_refresh_secs` / `client.force_update`）带默认值与范围；设备详情显示「设备详情 188 715 060 + 归属账号/平台/在线/最后在线 + 最近 10 条会话（requested）」；用户详情显示「账号 ID / 状态 active / 注册时间 / 设备数 2 + 该账号设备 + 最近会话（requested、authorized）」。
- 状态：P0-1..P0-4、P1-1..P1-4 全部 `- [x]`；唯一 `[~]` 是既有的 BLOCKED-E1（本机无 Xcode，flutter build macos 不可用），不属于 P0/P1。
- 下一步：无（计划内 P0/P1 已清零且 gate 全绿）。若要继续，可做的增量：管理端「会话详情」、客户端 Web 端文件上传走真实 `File` 选择器、PG 环境跑一遍 admin 契约测试。

## Round 4（2026-11-20 22:47–22:57，Ralph 第 4 轮）P2-1 登录页回正 + P2-2 文件传输页复刻
- 改动：`client/lib/pages/login_page.dart`（去掉手机号/邮箱大 Tab，恢复原型单表单；邮箱登录改为「邮箱登录」按钮 → 弹窗内真实调 `/v1/auth/email/code`、`/v1/auth/email/login`）、`client/lib/pages/files_page.dart`（队列 + 本机/远端双栏 + 选择文件/文本内容入口 + 远端列表下载/删除）、`client/test/p2_login_files_test.dart`（新增 3 个用例）
- 修复：邮箱弹窗用例在 FakeAsync 下真实写会话文件不完成 → 点击放到 `tester.runAsync` 内；清掉调试 print
- 验证：`dart analyze lib` 无 error/warning；`flutter test` 28 passed；`flutter build web` 通过
- 截图：`proto-s01-login.png`（单表单 + 微信/邮箱 V1 按钮）、`proto-s07-files.png`（队列 + 双栏 + 空态）
- 备注：轮次仍被调用上限截断，勾选与日志由 lead 补齐
- 下一步：P2-3 等待授权页 / P2-4 会话中 / P2-5 质量详情
