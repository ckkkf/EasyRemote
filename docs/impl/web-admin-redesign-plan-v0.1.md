# EasyRemote 管理端技术栈与原型整改方案 v0.1

> 编制日期：2026-11-22
> 目标：交给 DS 按任务卡分阶段执行管理端整改。
> 需求基线：`docs/prd/easyremote-prd-v0.8.md`、`docs/spec/pages.md`、`prototype/index.html`、仓库根目录 `AGENTS.md`。
> 本文是专项整改总纲，不是一个允许一次性改完所有内容的任务卡。

## 1. 结论

当前管理端**用了 React，但没有按冻结 PRD 落地要求的前端技术栈**：

- PRD §6.4 要求 React 18 + TypeScript + Vite + Tailwind + shadcn/ui；仓库 `web-admin/package.json` 使用 React 19，dependencies 只有 React/ReactDOM，未配置 Tailwind。
- PRD §6.7.1 要求 shadcn 组件源码复制到 `components/ui/`，业务组件位于 `components/business/`；当前没有这两个目录，按钮、表格、卡片、弹窗等主要由 `App.tsx` 内的本地组件和 `styles.css` 手写实现。
- `App.tsx` 集中登录、仪表盘、列表、配置、审计、详情等多个页面，当前页面路由是本地 state；未采用 PRD 指定的 React Router、TanStack Query、TanStack Table、Recharts、react-hook-form/Zod。
- 设计 token 已在 `design/generated/` 生成，但 `web-admin/src/main.tsx` 只导入 `./styles.css`；当前 CSS 自己定义变量和样式，没有接入 PRD 声明的生成 token + Tailwind/shadcn 主题链。

因此更准确的判断是：**管理端是一个可运行的 React 原型/功能壳，部分接口和权限/分页/中断业务已接通，但实现形态和页面闭合度没有满足 PRD 的 shadcn + Tailwind 规范，也没有完整按原型/页面清单落地。** 不能简单地说“完全没做需求”：已有登录、仪表盘、用户/设备/会话/审计/配置页面以及若干真实 API 行为，应在重构时保留并验证，不要无理由推倒 Go API。

shadcn/ui 的关键点：PRD 要求的是**把组件源码放进仓库维护**，不是添加一个 `shadcn/ui` npm runtime dependency。判断是否落地应检查 `components/ui/*` 的源码和 Tailwind 工具链，不能只看 package.json 是否有名为 shadcn 的包。

## 2. 可复核证据

| 项 | 当前证据 | 需求来源 |
|---|---|---|
| React 主版本不一致 | `web-admin/package.json:13-28`：React/ReactDOM `^19.3.1`，无 Tailwind、React Router、TanStack、Recharts、RHF、Zod 依赖 | PRD §6.4，`easyremote-prd-v0.8.md:655-673` |
| shadcn 源码结构缺失 | 当前 `web-admin/src` 只有 `App.tsx`、`api.ts`、`styles.css`、contract/error/capability 文件，无 `components/ui` / `components/business` | PRD §6.7.1，`easyremote-prd-v0.8.md:700-708` |
| 自定义组件/样式替代组件库 | `App.tsx:39-154` 定义 Card、Pill、Pagination、Modal 等；`styles.css:1-266` 自定义 shell、表格、按钮、输入、modal 等样式 | PRD §6.7.1 |
| 页面状态由一个大组件维护 | `App.tsx:1-1477` 包含多个页面、数据状态、事件处理和主框架；`main.tsx:1-10` 直接 render App | PRD §6.7.1 的业务/UI 组件分层 |
| token 链路未接入后台入口 | `main.tsx:1-5` 只导入 `styles.css`；生成文件 `design/generated/tokens.css` 与 Tailwind tokens 未进入后台构建 | PRD §6.7.3-6.7.4，`easyremote-prd-v0.8.md:739-757` |
| 仪表盘和原型/PRD 信息结构不一致 | `App.tsx:264-440` 每 30 秒刷新，主要展示设备/会话数、在线率、平台分布、最近会话/审计；PRD 要求 5 秒刷新、规模/质量/稳定性指标、24h/7d/30d 趋势和平台/版本下钻 | PRD §6.3.1，`easyremote-prd-v0.8.md:570-583`；页面 A2，`docs/spec/pages.md:201-208` |
| 用户列表闭合度不足 | `App.tsx:444-596` 当前表格仅账号 ID、脱敏手机号、状态、注册时间；筛选是 query/status；主要写操作只有封禁 | A3，`docs/spec/pages.md:210-217`；PRD §6.3.2 |
| 设备详情未达到客服排查规格 | `App.tsx:1249-1276` 是 modal，展示设备码/账号/平台/在线/最后在线和最近 10 条会话；没有 30 天质量曲线 | A6，`docs/spec/pages.md:229-232`；PRD §6.3.3 |
| 会话列表字段/过滤不足 | `App.tsx:721-889` 有 state 过滤和中断流程，但没有完整时间/账号/设备/链路/异常结束过滤及质量字段 | A7/A8，`docs/spec/pages.md:234-242` |
| 配置与审计只完成基础页 | 配置页有配置字段编辑和能力成熟度列表；契约/页面没有显示完整变更历史、一键回滚闭环；审计 action 下拉来自当前页 | A12/A13，`docs/spec/pages.md:256-266`；ER-801b 未完成项 |
| 原型明确是 shadcn 后台视觉稿 | `prototype/index.html:783-799` 标注后台 shadcn/ui；后台 6 屏为登录、仪表盘、用户列表、设备详情、会话列表、配置中心；页面索引见 `docs/spec/pages.md:295-346` | `prototype/index.html` 与 `docs/spec/pages.md` |

说明：`prototype/index.html` 是静态/交互演示，有示例用户、固定指标和 toast 文案。它只能作为布局、信息层级、组件视觉和交互参考；不能把示例数据、演示动作或页面里出现的额外功能直接搬进生产代码。

## 3. 页面/原型对照原则

原型已有的 6 个后台屏：

| 页面 | 原型 | 当前实现 | 整改要求 |
|---|---|---|---|
| A1 登录（TOTP） | `prototype/index.html` s12，约 1927 行 | 有真实登录表单，但自定义样式 | 迁移到 shadcn Form/Input/Button；保留真实 TOTP/API、错误 envelope、loading/error；删除页面里的开发账号和绕过 MFA 提示 |
| A2 仪表盘 | s13，约 1980 行 | 有概览与平台分布；缺趋势/质量/稳定性；30 秒刷新 | 按 PRD 的指标组和周期筛选重建；所有数值只能来自后端权威数据/telemetry，缺 API 时标记能力未提供，不准造数 |
| A3 用户列表 | s14，约 2122 行 | 有服务端分页、查询、封禁、详情 modal；显示字段/操作不全 | 用 Data Table 实现 PRD 列、服务端筛选和权限控制；缺字段/操作 API 时先开契约任务卡，不得用本地推导冒充 |
| A6 设备详情 | s15，约 2231 行 | 当前只有简化 modal + 最近会话 | 改为可直达的详情路由/页面，增加真实 30 天质量曲线、会话列表、错误/空态；曲线 API 缺失时标明 backend blocker |
| A7 会话列表 | s16，约 2363 行 | 有列表、状态筛选、kill | 按页面规格补列和真实筛选；详情入口进入 A8；中断使用当前 ER-812 的 partial/传播/审计结果，不回退为简单 toast |
| A12 配置中心 | s17，约 2463 行 | 有当前值编辑与能力 maturity 列表 | 匹配配置分组、作用域、默认值、历史、回滚；仅调用真实配置 API，写操作保留权限、审计、结果状态 |

原型清单没有 A4 用户详情、A5 设备列表、A8 会话详情、A9 中断确认、A13 审计日志等全部页面，但 `docs/spec/pages.md` 对其有明确要求。实现以页面 spec 为准，原型只补视觉样式，不因原型缺画就删除需求。

## 4. 执行边界

1. 这是管理端前端整改，不是重写 Go 服务端。优先复用现有 admin API、鉴权、权限、审计、中断传播、分页和 capability 数据。
2. 已由任务卡标记完成的行为必须先跑现有测试/审查 evidence 再迁移：ER-800 presence、ER-801b 分页、ER-810 身份/权限、ER-812 kill partial result。UI 改造不能回退这些行为。
3. 当前安全相关未完成项继续单独跟踪：ER-811 服务端 logout/token storage。视觉迁移不能把本地清 token 误写成服务端登出成功。
4. 任何 UI 所需字段/API 不存在时，按根 `AGENTS.md` 先建立 contract-first 后端任务卡。不得在前端造字段、直接改生成物、调整 spec 迁就现状或把 mock 值混入 production。
5. A10 风控中心按 PRD V1 分期；A11 版本管理虽在 PRD 后台章节列为 P0，但 API/发布能力必须先核实。A14 管理员与角色按现行页面清单和授权能力拆卡。无真实后端支撑的页面保持禁用/明确未开放，不得做成假入口。
6. 不做一次性大 PR。每张执行卡只覆盖一页或一个共享组件纵切，明确文件白名单、字段映射、测试和回滚。

任务卡冲突处理：仓库已有 `docs/impl/tasks/ADM-02-users.md`，其范围是用户列表响应信封收口，不等于用户页面整改。ADM-UI-00 必须先核对该卡状态与证据；不得覆盖、改写或重复执行它限定的后端改动。ADM-UI-05 只处理页面字段、交互和视觉迁移；若依赖 ADM-02 尚未完成，先等其完成或按负责人批准拆分依赖，不要把两张卡合成一张。

## 5. 目标前端结构

```text
web-admin/src/
  app/
    providers.tsx             # QueryClient、权限/身份上下文
    router.tsx                # React Router 路由与权限守卫
  components/
    ui/                       # 复制到仓库的 shadcn/Radix 源码组件
    business/                 # AdminShell、DataTable、PageHeader、Metric 等业务组合
  features/
    auth/
    dashboard/
    users/
    devices/
    sessions/
    audit/
    config/
  lib/
    api/                      # 现有契约生成 DTO/API adapter
    permissions/
    formatters/
  styles/
    globals.css               # shadcn semantic vars + generated primitive tokens
```

按 PRD 逐步引入 React Router、TanStack Query、TanStack Table、Recharts、react-hook-form/Zod。不要为了“采用库”机械套库：每项依赖必须对应实际页面需求和测试；但 PRD 明确指定的 shadcn/Tailwind/React 18 不得再次被手写组件替代。

shadcn 使用规则：

- `components/ui/*` 保存复制进仓库的 primitives；不得安装一个黑盒 shadcn runtime 包代替源码管理。
- `components/business/*` 只组合 ui 原子组件；页面 feature 负责业务状态、API 和页面编排。
- 主题语义变量放 `globals.css`，primitive 从 `design/generated/tokens.css` / `tailwind.tokens.cjs` 导入或映射；禁止重复维护第二套颜色/间距/圆角。
- 业务表格统一使用 Data Table 模式 + 服务端分页/筛选/排序；不要复制粘贴 HTML table 实现。
- 组件需支持键盘、focus、disabled、loading、错误提示、ARIA；图标按钮提供 tooltip/accessible name。
- 视觉以原型 1360×860 为主基线，运行截图对照；另验收 1280×800、1024 和 768 宽度，表格横向溢出有明确策略。

## 6. 分阶段任务序列

以下卡号是建议编号。DS 开始时先检查是否已有同号任务卡；有则尊重其状态并基于当前代码更新计划，不覆盖历史记录。每张卡需单独创建任务卡，遵循仓库 `AGENTS.md` 的字段映射/文件白名单/测试/gate 规则。

### ADM-UI-00：基线与页面验收清单

- 整理原型 6 屏、页面 spec A1-A14 与当前实现的可追踪矩阵；给每项标记 `implemented / partial / missing / blocked by API`。
- 获取当前 admin frontend tests/build/gate 输出；保留现有用户工作区变更。
- 为 6 个原型屏建立 1360×860 Playwright 截图基线；确认字体、viewport、主题和 fixture 可复现。
- 验收：矩阵每一行有 PRD/pages 行号、代码文件行号、对应测试或缺失卡号；不得把示例数据记作功能完成。

### ADM-UI-01：技术栈收口与 shadcn/Tailwind 基础

- 将 React/ReactDOM 与 types 对齐 PRD React 18；锁文件保持可复现。
- 配置 Tailwind、PostCSS、必要构建插件和 shadcn CLI 配置；将首批原子组件源码复制进 `components/ui/`，不是安装黑盒组件包。
- 接入生成 token：建立全局 semantic CSS vars、Tailwind tokens 映射、light/dark 明确策略。
- 清理自定义 CSS 中与 shadcn primitives 重复的 button/input/dialog/table/card 基础样式；业务布局可保留必要 utility/少量 CSS。
- 验收：无业务页回归；`npm ci`、typecheck、tests、build 通过；token 修改生成检查通过；源码中能找到真实 shadcn primitives 的使用。

### ADM-UI-02：App Shell、路由、数据状态与共享组件

- 建立 React Router 路由、权限守卫、Query provider、错误边界；刷新/深链可直接进入页面。
- 迁出 `App.tsx` 的主框架与页面；构建 shadcn AdminShell（sidebar、group nav、breadcrumb、global search、环境标识、用户菜单），布局参考原型。
- 建立 `components/business/DataTable`、PageHeader、Loading/Skeleton、EmptyState、ErrorState、ConfirmDialog、Toast/sonner 共享组件。
- 统一 API query key、取消、重试策略、轮询、错误 trace_id 展示；复用当前 generated contract/types，不改接口语义。
- 验收：路由和权限有测试；刷新不回登录/首页；各 route 的 loading/empty/error/success 均可验证；组件无重复样式来源。

### ADM-UI-03：A1 登录与身份/权限迁移

- 用 shadcn Input/Button/Form/Alert 构建 TOTP 登录；保留真实 admin API 和 `Envelope` 错误。
- 角色/username/permissions 必须取真实登录身份；保留 ER-810 的 useCan 行为，不仅隐藏按钮，也依赖服务端授权。
- 移除 login 页面内的开发口令和 MFA 绕过指导；dev 登录只能由显式 development 配置页面/文档给出，production UI 不展示。
- 登出沿用或完成 ER-811 服务端撤销，不可只清 local state 后显示成功。
- 测试：登录成功/错误/MFA required/限流/登出失效/readonly 写操作不可见且 API 拒绝。

### ADM-UI-04：A2 仪表盘

- 结构跟原型：指标卡、趋势区域、平台/版本筛选、最近会话/质量概览；内容字段逐项映射 PRD §6.3.1 和 pages A2。
- 把 PRD 的 5s 更新实现为可取消轮询/手动暂停，显示最后更新时间和 stale/error 状态；禁止组件重复请求。
- 接入真实 Recharts 图表，时间段 24h/7d/30d；筛选条件发给服务端。
- 当前只可展示接口提供且语义有效的 metrics；缺少数据接口时先立契约/后端卡，前端显示“数据未提供”而不是 0、样例百分比、mock 曲线。
- 测试：5s polling 可暂停/卸载取消；时间段和平台/版本参数正确；空/部分指标缺失/接口失败有明确状态。

### ADM-UI-05：A3 用户列表 + A4 用户详情

- 用户页用 Data Table；列/筛选/操作按 `pages.md A3` 和 PRD §6.3.2，手机号持续默认脱敏。
- 支持账号/手机号/设备码/昵称/状态/时间范围查询；若后端没有某筛选字段先建 contract-first 卡。
- 查看详情改为独立 route 或符合设计的 Sheet/Drawer，包含账号信息、设备、最近 20 会话质量、操作记录和备注。
- 写操作分别定义权限、原因、确认、loading/success/partial/error；封禁、解封、踢下线、备注、导出缺 API 时不显示可点击假按钮。
- CSV 导出走服务端并写审计，不在前端导出当前页冒充全量导出。
- 测试：服务端 query 参数、空态、脱敏、分页、RBAC、写操作审计、导出审计。

### ADM-UI-06：A5 设备列表 + A6 设备详情

- 列和筛选按 `pages.md A5`：设备码、设备名、账号、平台、系统/客户端版本、last seen、NAT；在线筛选必须使用权威 presence 分页能力。
- A6 从 modal 详情提升为可直达页面；布局参考 prototype s15；质量曲线覆盖最近 30 天，最近会话可下钻。
- 详情曲线必须来自真实设备 telemetry，支持区间、无数据、部分数据和失败状态；缺 telemetry API 就先出阻塞卡。
- 强制下线、解绑、风险标记仅在 capability/权限/API 真正支持时显示。
- 测试：直接 URL、返回列表状态保留、presence 错误、分页/筛选、30 天曲线单位/时间轴、无数据态。

### ADM-UI-07：A7 会话列表 + A8 会话详情 + A9 强制中断

- 会话列表字段和筛选完整按 `pages.md A7`；服务端分页；列配置/排序由 TanStack Table 管理。
- A8 展示每秒质量曲线、链路切换、错误码时间线和双方设备信息；无真 telemetry 不渲染假曲线。
- A9 使用 shadcn AlertDialog/Dialog：原因类别 + 自定义说明 + 二次确认；展示传播/审计/部分失败结果。
- 复用 ER-812 closed/propagated/audited/warning 的语义；等待真实后端返回，不能提前显示“双方已断开”。
- 测试：筛选参数、会话详情深链、kill success/partial/timeout/forbidden、结果与审计刷新一致。

### ADM-UI-08：A12 配置中心 + A13 审计日志

- 配置页显示分组、key、当前/默认值、平台/版本 scope、变更历史；修改须显示 diff、校验错误、保存状态和回滚入口。
- 一键回滚必须来自真实后端版本接口；接口缺失先建任务卡，不能用浏览器本地还原冒充服务端 rollback。
- 审计页显示时间/actor/role/action/object/before-after/IP/result；时间范围、actor/action/object 服务端过滤；append-only 只读。
- 修复 action 选项只来自当前页的问题：服务端需提供合法 action options 或可搜索值；不要暗中取固定上限集合。
- 测试：服务端参数、权限、变更审计对账、rollback 失败、审计无编辑/删除。

### ADM-UI-09：A10 风控、A11 版本、A14 管理员与角色

- 先核对 PRD MVP/V1 优先级和 Go API 实际能力，按独立页面卡交付，不并入 UI 基础迁移。
- A10 按 V1 分期；无告警/黑名单/爆破记录数据源时保持未开放。
- A11 按 PRD 版本管理要求先核实发布/灰度/强制升级 API，避免单靠前端做“发布”。
- A14 使用真实管理员列表/角色/RBAC/MFA 状态服务；账号禁用/角色修改为 audited actions。
- 测试：权限最小化、写操作审计、feature maturity gate、不可用时无假操作入口。

### ADM-UI-10：视觉、可访问性和回归验收

- 所有页面在 1360×860 与至少 1280×800 完成 screenshot compare；再验收 1024/768 responsive，不出现文本溢出、表格遮挡、交互目标重叠。
- 视觉验收守不变量：原型信息层级/导航/密度、token 单一来源、shadcn primitives 单一交互反馈、页面状态完整；不要求逐像素复制原型里的静态假数据。
- Playwright 覆盖真实登录/列表过滤/分页/详情/中断 partial/登出；fixture 只用于测试，不得打包进 release 数据流。
- 键盘导航、focus-visible、Dialog focus trap、ARIA labels、色彩对比和 reduced motion 检查。
- 验收命令按项目当前配置执行：`npm ci`、`npm run typecheck`、`npm test -- --run`、`npm run build`、`bash scripts/gate.sh`。

## 7. 全局不可妥协项

- 不得只换配色或加一个 shadcn Button 就声称完成技术栈迁移。
- 不得保留大块 `App.tsx` + 单个手写 CSS，再把几个 class 名称改成 Tailwind。
- 不得同时运行旧自定义视觉层和 shadcn layer 覆盖同一属性；逐页迁移并删除旧样式。
- 禁止使用 prototype 示例数据、固定百分比、假趋势和 `data-toast` 演示动作作为真实 UI。
- 禁止在 UI 新增 PRD/spec 未允许的业务操作；原型截图中的“新建账号/节点”等只有 contract/API 支持且需求允许时才实现。
- 禁止 mock production API、静默吞请求错误、用 toast 假装写操作成功。
- 禁止为配合 UI 把 `docs/spec/*` 改成当前实现形状。所有 API 扩展都 contract-first、生成后再改 Go 和前端。
- 保留当前用户未提交修改；特别是开工前先检查 `git status --short`，不要覆盖 `web-admin/vite.demo.config.ts` 上已有的改动。

## 8. DS 执行节奏与交付标准

1. 先读根 `AGENTS.md`、本文、PRD §6、`docs/spec/pages.md` A1-A14、原型对应后台屏、现有 admin task/evidence。
2. 先建立 ADM-UI-00 页面矩阵和失败/缺口证据；不可直接大规模重写。
3. 一次只执行一个 ADM-UI 任务卡；如果卡片缺字段映射/白名单/测试就先补卡，不写实现。
4. 每卡先测试后实现；完成后跑 web-admin tests/build 与 `bash scripts/gate.sh`。
5. 使用 `docs/impl/evidence/ADM-UI-<NN>/` 保存截图、命令输出、视觉判据和未完成项；不保存敏感账号/生产数据。
6. 一卡完成后继续下一卡，但不可越过未完成的 API/security prerequisite；遇到 blocked-by-API 保留 disabled 状态并创建准确后端卡。
7. 最终只能在技术栈、原型覆盖、页面 spec、E2E、视觉检查和 gate 均有证据后写“完成”。

每张卡最终回复必须列：改动文件、React/shadcn/Tailwind 使用证据、字段映射、原型/spec 对照、测试名与输出、截图/几何判据、gate 输出、未完成与回滚。

## 9. 字段映射

本文件是前端架构与页面整改计划，没有新增 API 字段，因此当前字段映射为 `N/A`。后续每张执行卡必须逐字段引用 `docs/spec/contract.yaml`、`docs/spec/pages.md` 或对应现行 PRD 行号；遇到契约缺字段先拆 API 卡。
