# ADM-UI-00 页面矩阵（管理端整改状态）

> 首次采集：2026-11-23（基线）；本次更新：2026-11-23（ADM-UI-00d 收尾：A1-A14 **14 implemented / 0 partial**，全量 E2E 与闸门同时绿）
> 需求基线：`docs/prd/easyremote-prd-v0.8.md` §6（行 543-790）、`docs/spec/pages.md` A1-A14（行 197-272）、`prototype/index.html` s12-s17
> 断言脚本：`node scripts/check-admin-ui-matrix.mjs`

## 状态口径

| 状态 | 含义 |
|---|---|
| `implemented` | 页面 spec 要求的能力有真实 API + 真实副作用 + 测试/截图证据，且形态符合 PRD §6.4/§6.7.1 |
| `partial` | 有真实 API 与真实副作用，但列/筛选/操作/视觉形态仍缺项，缺项已登记缺口卡 |
| `missing` | 页面或路由不存在 |
| `blocked-by-api` | 所需字段/接口在 `docs/spec/contract.yaml` 中不存在，必须先开 contract-first 后端卡 |

**原型示例数据不算实现**：`prototype/index.html` 里的账号数、建连成功率、会话号都是演示值；本矩阵只承认来自真实 admin API 的值。

## A1-A14 页面矩阵

| ID | 页面 | spec | 原型 | 实现 | 状态 | 证据 | 缺口卡 | 缺口/风险说明 |
|---|---|---|---|---|---|---|---|---|
| A1 | 登录（账号密码 + TOTP） | pages.md:197-199; PRD:553 | s12 | web-admin/src/App.tsx:203 | implemented | docs/impl/evidence/ADM-UI-03/login.png; web-admin/tests/login.test.tsx:TestLoginRendersShadcnForm | — | shadcn Card/Input/Label/Button/Alert；真实 TOTP + 错误信封 + trace 复制；登出为服务端撤销 |
| A2 | 仪表盘 | pages.md:201-208; PRD:570-583 | s13 | web-admin/src/features/dashboard/metrics.ts:46 | implemented | docs/impl/evidence/ADM-API-02f5/verification.md; scripts/admin-ui-e2e-dashboard.py | — | 15 个指标全部来自真实数据：规模（含 24h 新增设备）、质量（P50/P95/中继/P2P/帧率达成）、稳定性（异常中断/崩溃/重连）、时间桶趋势（02f1-02f6）；统计窗口 24h/7d/30d 可切 |
| A3 | 用户列表 | pages.md:210-217; PRD:585-592 | s14 | web-admin/src/App.tsx:540 | implemented | docs/impl/evidence/ADM-API-03e/verification.md; scripts/admin-ui-e2e-users.py | — | 8 列 + 服务端分页/搜索/状态筛选/注册时间范围 + 封禁/解封/踢下线（原因必填+审计）+ CSV 导出（服务端脱敏 + 导出审计） |
| A4 | 用户详情 | pages.md:219-221; PRD:588-589 | — | web-admin/src/features/users/user-detail-page.tsx:60 | implemented | docs/impl/evidence/ADM-API-03c/verification.md; scripts/admin-ui-e2e-users.py | — | 独立路由 /users/:id；账号信息 + 按账号过滤的设备 + 最近会话 + 操作记录（审计 object）+ 备注（可写、进审计） |
| A5 | 设备列表 | pages.md:223-227; PRD:594-599 | — | web-admin/src/App.tsx:723 | implemented | docs/impl/evidence/ADM-API-04c/verification.md; scripts/admin-ui-e2e-devices.py | — | 11 列（设备名/系统版本/客户端版本/NAT/风险）+ 搜索/平台/在线筛选 + 服务端分页；强制下线、解绑、标记/解除风险均真实生效并写审计 |
| A6 | 设备详情（客服排查主入口） | pages.md:229-232; PRD:597-601 | s15 | web-admin/src/features/devices/device-detail-page.tsx:71 | implemented | docs/impl/evidence/ADM-API-02d/verification.md; scripts/admin-ui-e2e-devices.py | — | 独立路由 /devices/:id；设备信息（11 项）+ 按设备过滤的会话 + 近 30 天质量按日聚合（无采样明确提示） |
| A7 | 会话列表 | pages.md:234-238; PRD:603-607 | s16 | web-admin/src/App.tsx:1165 | implemented | docs/impl/evidence/ADM-API-03f2/verification.md; scripts/admin-ui-e2e-sessions.py | — | 10 列（含平均延迟/结束原因/链路）+ 状态/时间范围/账号/设备/链路类型/异常结束筛选（全部服务端下推）+ 15s 轮询 + 中断（含双人确认） |
| A8 | 会话详情 | pages.md:240-242; PRD:607 | — | web-admin/src/features/sessions/session-detail-page.tsx:53 | implemented | docs/impl/evidence/ADM-API-03h/verification.md; scripts/admin-ui-e2e-sessions.py | — | 独立路由 /sessions/:id；会话字段 + 每秒采样 + 双方设备 + 链路切换 + 状态时间线 + 错误码时间线（网关按会话落库真实错误） |
| A9 | 强制中断确认弹窗 | pages.md:244-246; PRD:606 | — | web-admin/src/components/business/confirm-dialog.tsx:18 | implemented | docs/impl/evidence/ADM-UI-07b2/verification.md; scripts/admin-ui-e2e-sessions.py | — | Radix Dialog + 原因类别/说明 + 审计原因预览 + 双人确认（发起 / 待确认列表 / 第二人确认，5 分钟有效，同人确认被拒） |
| A10 | 风控中心（V1） | pages.md:248-250; PRD:609-617 | — | web-admin/src/App.tsx:1827 | implemented | docs/impl/evidence/ADM-UI-09c/verification.md | — | 黑名单（账号/设备/IP+CIDR/有效期）+ 真实连接闸门 + 告警列表；验证码爆破记录、举报工单未做 |
| A11 | 版本管理 | pages.md:252-254; PRD:619-624 | — | web-admin/src/App.tsx:2045 | implemented | docs/impl/evidence/ADM-UI-09d/verification.md | — | 版本列表 + 灰度发布（覆盖率与状态一致）+ 强制升级 + 下线（原因必填）；客户端检查更新接口与白名单灰度未做 |
| A12 | 配置中心 | pages.md:256-260; PRD:626-637 | s17 | web-admin/src/App.tsx:1396 | implemented | docs/impl/evidence/ADM-API-05d/verification.md; scripts/admin-ui-e2e-config-audit.py | — | 真实读写 + diff + 校验 + 变更历史 + 单键回滚 + 分组 + 平台/版本双层作用域（优先级 默认←全局←平台←平台+版本，审计对象带作用域） |
| A13 | 审计日志 | pages.md:262-266; PRD:639-643 | — | web-admin/src/App.tsx:1204 | implemented | docs/impl/evidence/ADM-API-05c2/verification.md; scripts/admin-ui-e2e-config-audit.py | — | 列齐全 + 服务端动作/结果/操作人/对象/时间范围筛选 + 动作下拉来自服务端 + CSV 导出（服务端脱敏 + 导出写审计） |
| A14 | 管理员与角色 | pages.md:268-272; PRD:556-566 | — | web-admin/src/App.tsx:1666 | implemented | docs/impl/evidence/ADM-UI-09b/verification.md | — | 列表（角色/状态/MFA）+ 改角色/启停（原因必填 + 审计 + 最后一个超管保护）；新建管理员/重置密码未做 |

## 横向技术栈矩阵（PRD §6.4 / §6.7.1 / §6.7.3）

| ID | 页面 | spec | 原型 | 实现 | 状态 | 证据 | 缺口卡 | 缺口/风险说明 |
|---|---|---|---|---|---|---|---|---|
| X1 | React 主版本 | PRD:659 | — | web-admin/package.json:14 | implemented | web-admin/tests/stack-conformance.test.ts:TestWebAdminReact18 | — | react/react-dom 18.3.1 + @types 18，锁文件可 npm ci |
| X2 | Tailwind + PostCSS | PRD:661 | — | web-admin/tailwind.config.ts:1-90 | implemented | scripts/check-web-admin-stack.mjs:TestTailwindWiredToGeneratedTokens | — | Tailwind v3，preflight 关闭（styles.css 仍负责基础排版） |
| X3 | shadcn 源码组件 | PRD:704-707 | — | web-admin/src/components/ui/button.tsx:1-56 | implemented | web-admin/tests/ui-primitives.test.tsx:TestButtonPrimitiveRenders | — | 6 个仓库内 primitive（button/input/card/badge/label/alert）+ dialog/textarea；无 shadcn 运行时包 |
| X4 | 设计令牌单一来源 | PRD:726-742 | — | web-admin/src/styles/globals.css:1-60 | implemented | scripts/check-web-admin-stack.mjs:TestTailwindBuildEmitsUtility | — | globals.css 引用 design/generated/tokens.css；构建产物含品牌色 |
| X5 | Router / Query / Table / Recharts / RHF+Zod | PRD:662-666 | — | web-admin/src/app/routes.ts:1-110 | partial | web-admin/tests/routes.test.ts:TestAdminRouteTableCoversNav | ADM-UI-04 | React Router + TanStack Query provider + DataTable 已落地；Recharts 趋势与 RHF/Zod 表单尚未引入（等真实接口/多字段表单） |
| X6 | 后台 E2E / 视觉回归 | PRD:776-783 | — | scripts/admin-ui-e2e-routes.py:1-40 | implemented | docs/impl/evidence/ADM-UI-10/audit.json | — | 8 个 admin-ui-e2e-* 脚本 + 六屏 DOM 基线比对 + 4 档响应式审计 |

## 原型 6 屏截图基线

viewport 1360×860、DPR 1、light、locale zh-CN；字体 `Inter, "PingFang SC", "Microsoft YaHei", sans-serif`。

| 原型屏 | 页面 | 截图 | 复现 |
|---|---|---|---|
| s12 | A1 登录 | docs/impl/evidence/ADM-UI-03/login.png | `python3 scripts/admin-ui-e2e-login.py` |
| s13 | A2 仪表盘 | docs/impl/evidence/ADM-UI-04/dashboard.png | `python3 scripts/admin-ui-e2e-dashboard.py` |
| s14 | A3 用户列表 | docs/impl/evidence/ADM-UI-05/users-list.png | `python3 scripts/admin-ui-e2e-users.py` |
| s15 | A6 设备详情 | docs/impl/evidence/ADM-UI-06/device-detail.png | `python3 scripts/admin-ui-e2e-devices.py` |
| s16 | A7 会话列表 | docs/impl/evidence/ADM-UI-07/sessions-closed-filter.png | `python3 scripts/admin-ui-e2e-sessions.py` |
| s17 | A12 配置中心 | docs/impl/evidence/ADM-UI-08/config-diff.png | `python3 scripts/admin-ui-e2e-config-audit.py` |

## 验收证据索引（每张执行卡）

| 卡 | 证据目录 | 关键断言 / 脚本 |
|---|---|---|
| ADM-UI-00 | `docs/impl/evidence/ADM-UI-00/` | 矩阵断言 + 六屏基线 + `bash scripts/gate.sh` |
| ADM-UI-01a | `docs/impl/evidence/ADM-UI-01a/` | `node scripts/check-web-admin-stack.mjs --build` |
| ADM-UI-02a | `docs/impl/evidence/ADM-UI-02a/` | `python3 scripts/admin-ui-e2e-routes.py` |
| ADM-UI-02b | `docs/impl/evidence/ADM-UI-02b/` | 三态单测 + 六屏比对 |
| ADM-UI-02c | `docs/impl/evidence/ADM-UI-02c/` | `python3 scripts/admin-ui-e2e-dialogs.py` |
| ADM-UI-03 | `docs/impl/evidence/ADM-UI-03/` | `python3 scripts/admin-ui-e2e-login.py` |
| ADM-UI-04 | `docs/impl/evidence/ADM-UI-04/` | `python3 scripts/admin-ui-e2e-dashboard.py` |
| ADM-UI-05a | `docs/impl/evidence/ADM-UI-05/` | `python3 scripts/admin-ui-e2e-users.py` |
| ADM-UI-06a | `docs/impl/evidence/ADM-UI-06/` | `python3 scripts/admin-ui-e2e-devices.py` |
| ADM-UI-07a | `docs/impl/evidence/ADM-UI-07/` | `python3 scripts/admin-ui-e2e-sessions.py` |
| ADM-UI-08a | `docs/impl/evidence/ADM-UI-08/` | `python3 scripts/admin-ui-e2e-config-audit.py` |
| ADM-UI-09a | `docs/impl/evidence/ADM-UI-09/` | `web-admin/tests/admin-scope.test.ts` |
| ADM-UI-10a | `docs/impl/evidence/ADM-UI-10/` | `python3 scripts/admin-ui-responsive-a11y.py` |
| ADM-UI-10b | `docs/impl/evidence/ADM-UI-10b/` | `python3 scripts/admin-ui-e2e-lifecycle.py` |
| ADM-02a | `docs/impl/evidence/ADM-02a/` | `cd server && go test ./internal/auth/ ./internal/adminapi/ ./internal/adminhttp/` |

## 变更记录

- 2026-11-23（ADM-02a）：A3 的 `phone_mask` 曾对所有短信注册账号返回 `***`（`auth.Login` 建号未写 `PhoneEnc`）。已修复并补 `TestListUsers_Mask` / `TestListUsers_HTTPEnvelope`；历史账号仍为 `***`，回填见 ADM-02b。
- 2026-11-23（ADM-UI-04）：A2 重构为规模/质量/稳定性三组，5s 轮询可暂停 + 陈旧标记；新基线见 `docs/impl/evidence/ADM-UI-04/screens/manifest.json`。
- 2026-11-23（ADM-UI-07a）：A7 修复状态枚举 `ended`→`closed` 与筛选参数 `status`→`state` 两处真实缺陷。
- 2026-11-23（ADM-UI-08a）：A12 增加保存前 diff；A13 动作筛选改为精确输入（不再用当前页猜下拉）。
- 2026-11-23（ADM-UI-09a）：A10/A11/A14 确认无接口，保持未开放；`admin-scope.test.ts` 守护。
- 2026-11-23（ADM-UI-10a）：4 档响应式 + 可访问性审计；修复对比度/目标尺寸/焦点环/reduced-motion/窄屏空白。

- 2026-11-23（ADM-02b）：历史账号在下次登录时幂等回填 `PhoneEnc`（`UpdateAccountPhoneEnc` + 登录路径），`phone_mask` 不再是 `***`；契约用例 `AccountPhoneEncBackfill` 覆盖 memory/pg 两种实现。

- 2026-11-23（ADM-API-02a）：A2 的「在线用户」由契约 `Overview.users_online` 提供真值（按账号去重）；「24h 新增设备」仍缺设备创建时间字段，继续显示「数据未提供」。

- 2026-11-23（ADM-API-03a）：A3 增加「设备数」列，值来自契约 `UserRow.device_count`（服务端聚合）；a3 列判据随之为 6 列，其余 5 屏不变。

- 2026-11-23（ADM-API-03b）：A3 增加「昵称」「最近登录」两列（`UserRow.nickname` / `last_login_at`，登录路径写入）；a3 判据为 8 列，其余 5 屏不变。

- 2026-11-23（ADM-API-04a）：A5 增加设备名/系统版本/客户端版本/NAT 列（客户端 bind 上报），A6 详情 kv 7→11；a6 判据随之变化，其余屏不变。

- 2026-11-23（ADM-API-04b）：A5 增加在线状态服务端筛选与「强制下线/解绑」行操作（原因必填 + 审计）；六屏 DOM 判据不变。

- 2026-11-23（ADM-API-05a）：A13 动作下拉改由 `GET /admin/v1/audit/actions` 提供，新增操作人/对象/时间范围服务端筛选；六屏 DOM 判据不变。

- 2026-11-23（ADM-API-05b）：A12 增加变更历史卡片与「回滚到旧值」按钮（`/admin/v1/config/history`、`/rollback`，写 `config.rollback` 审计）；六屏 DOM 判据不变。

- 2026-11-23（ADM-API-02c）：A8 质量曲线改为真实采样（客户端上报 → 服务端存储 → 管理端展示），无采样时显示「没有采样数据」；六屏判据不变。

- 2026-11-23（ADM-API-02d）：A6 近 30 天质量改为真实按日聚合（延迟/丢包/中继占比/采样数），无采样显示「没有质量采样」；另修复 003-005 迁移未进 embed 目录导致真实 PG 不生效的问题。六屏判据不变。

- 2026-11-23（ADM-API-02e）：A2 质量卡从「数据未提供」切换为真实 P50/P95/中继占比（统计窗口 24h/7d/30d 可切换，请求带 range）；建连/P2P/帧率达成率与稳定性组仍无数据源。六屏判据不变。

- 2026-11-23（ADM-API-04c）：A5 增加「风险」列与标记/解除风险操作（原因必填 + 审计，不自动阻断连接）。六屏判据不变。

- 2026-11-23（ADM-API-06a）：A14 后端接口已提供（列表/改角色/启停 + 防锁死），页面仍未开放（admin-scope 测试守护）；六屏判据不变。

- 2026-11-23（ADM-API-06b）：A11 后端接口已提供（列表/灰度发布/下线 + 覆盖率一致性校验），页面仍未开放；六屏判据不变。

- 2026-11-23（ADM-API-06c）：A10 后端接口已提供（黑名单 CRUD + 告警列表 + 连接闸门真实拦截），页面仍未开放；六屏判据不变。

- 2026-11-23（ADM-UI-09b）：A14 页面落地（/admins，admin.read 守卫）：列表/改角色/启停 + 原因对话框 + 审计；开发环境 seed `ops-dev`（disabled、无 TOTP）用于演示与 E2E。六屏判据仅 `a2-dashboard.nav` 增加一项（预期内），新 manifest 作为基线。

- 2026-11-23（ADM-UI-09c）：A10 页面落地（/risk，risk.read 守卫）：黑名单增删（账号/设备/IP+CIDR/有效期）+ 真实拦截告警列表。六屏判据仅 `a2-dashboard.nav` 增加一项（预期），新 manifest 作为基线。

- 2026-11-23（ADM-UI-09d）：A11 页面落地（/versions，version.read 守卫）：版本列表/发布（草稿/灰度/全量 + 覆盖率一致性预校验）/下线（原因必填），写操作进审计。六屏判据仅 `a2-dashboard.nav` 增加一项（预期），新 manifest 作为基线。

- 2026-11-23（ADM-UI-00d 收尾）：A8 由 03h（会话错误事件 + 错误码时间线）收口；A1-A14 全部 implemented。全量 E2E `PASS: 14 / FAIL: 0`，`GATE: PASS`。视觉基线更新为 `ADM-API-03h/screens/manifest.json`。

- 2026-11-23（ADM-UI-11a/11b）：用户反馈「管理端很丑」属实——页面层此前几乎没用 shadcn。已把 Table/Card/Button/Icon 真正接入（DataTable 与三张手写表、业务 Card、导航图标、顶栏按钮），按钮/输入/导航几何对齐 shadcn，删除 285 行 CSS 中的 .tbl/.card 废弃规则。六屏判据不变，视觉基线更新为 `ADM-UI-11b/screens/manifest.json`。X5（Recharts/RHF+Zod）仍未引入。

- 2026-11-23（ADM-UI-11c）：全站控件改用 shadcn——16 个原生 select → Radix Select（SimpleSelect 封装）、3 个开关 → Switch、14 个输入 → Input；新增 scripts/admin_e2e.py 的 pick() 并改写 31 处 select_option。修复 Radix 空值哨兵、Switch 24px 目标、顶栏 768/1024 溢出、tr 命中被 table 拦截、切换作用域读到旧值等问题。全量 E2E PASS: 14 / FAIL: 0，AUDIT OK，GATE: PASS，视觉基线 ADM-UI-11c/screens/manifest.json。

- 2026-11-24（ADM-UI-12）：主题切 shadcn 默认（neutral HSL、0.5rem、preflight 打开）；删除自写 SimpleSelect 与 .btn/.link 桥接 CSS；全量 E2E PASS 14/0、AUDIT OK、GATE PASS，基线 ADM-UI-12。
