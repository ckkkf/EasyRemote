# UI 对齐与功能补全（第二轮）

> 2026-11-20 ｜ 起因：反馈「UI 没有按原型复刻 / 管理端功能不完善 / 客户端不闭合」

## 一、客户端（Flutter）

### 框架与导航
- 新增 `client/lib/shell.dart`：桌面端 232px 侧边导航，**图标 + 文字**，选中态为浅蓝胶囊 + 主色文字 + 右侧圆点；悬停态、logo、版本号、底部账号信息齐全。
- 窄屏（< 900px）自动切换为底部 `NavigationBar`，五项均带文字标签；页面标题收进页头，避免与 AppBar 重复。
- `PageHeader` 统一页头（标题 + 副标题 + 右侧操作），窄屏自动补外观切换按钮。

### 外观
- `theme.dart` 按 `design/tokens.json` 重建：浅/深两套 ColorScheme、字号阶梯、卡片/按钮/输入框/导航主题。
- 三态主题切换（跟随系统 / 浅色 / 深色），侧边栏底部与设置页都可切换，全局 `erThemeMode` + `ValueListenableBuilder` 即时生效。
- 支持 `?theme=light|dark|system` 覆盖初始外观（截图与调试用）。

### 页面
| 页面 | 复刻要点 | 真实数据 |
|---|---|---|
| 登录 | 左侧品牌渐变 + 三条卖点 + 版本；右侧表单、协议勾选、微信/邮箱 V1 占位；窄屏堆叠 | `/v1/auth/sms`、`/v1/auth/login`、`/v1/devices/bind` |
| 我的设备 | 本机设备码渐变卡 + 复制、搜索、全部/在线/离线筛选、设备卡片（平台/状态/3-3-3 设备码/连接）、「其他连接方式」入口 | `/v1/devices` |
| 连接设备 | 9 位 3-3-3 分段输入（透明输入层 + 9 个格子，焦点唯一，不再逐格丢字）、验证码、请求连接、最近连接、错误信封 | `/v1/connect/request`（带 `X-Device-Token` 与 captcha） |
| 会话 | 等待授权三步进度、状态、会话 ID、返回/进入画面 | `/v1/connect/status` |
| 网络诊断 | 6 个指标卡（时延、丢包、链路、NAT、打洞、加密）+ 建议列表 | 5 次 ping 网关实测（中位数/丢包率） |
| 设置 | 外观（三态）、画质、隐私屏、阻止睡眠、本机设备码、版本 | 本地状态 + 设备信息 |

### 已修的实打实 bug
1. `requestConnect` / `sessionStatus` 未带 `X-Device-Token` → 服务端 `700005`，UI 只显示 request failed。
2. 首次连接缺验证码入口 → 服务端强制 CAPTCHA，UI 无从输入。
3. `DeviceInfo` 解析用了 `id`/`device_code`，列表接口实际给 `raw_code` / 无 `id` → 本机标识失效。
4. 逐格 TextField 在 Web/移动端焦点竞争导致丢字 → 改为单一透明输入层。

## 二、管理后台（React）

- 整体重写 `web-admin/src/App.tsx` 与 `styles.css`（不引新依赖）。
- 登录：分栏品牌页 + 错误信封（code/name/message/trace_id）。
- 布局：232px 侧边导航（图标 + 文字）+ 顶栏 + 退出登录。
- 仪表盘：4 个指标卡、在线率进度条、平台分布条形图、最近 8 条会话、最近 8 条审计、更新时间、手动刷新、30 秒自动刷新开关。
- 用户：搜索、状态筛选、分页、封禁弹窗（原因 >= 4 字）、操作反馈横幅。
- 设备：搜索、在线/平台筛选、分页、3-3-3 设备码、最后在线相对时间、复制设备码。
- 会话：状态筛选、15 秒自动刷新、持续时长、中断弹窗（写审计）。
- 审计：动作/结果筛选、搜索操作人/对象/IP、分页、结果徽标、before→after 等宽展示。
- 修正 `api.ts` 的 `DeviceRow`：接口字段是 `last_seen`，之前类型写成 `last_seen_at`，导致「最后在线」全是 —。

## 三、验证

| 项 | 结果 |
|---|---|
| `dart analyze lib` | 0 error / 0 warning（仅 Material 弃用提示） |
| `npm run typecheck` | 通过 |
| `npm run build` | 通过（dist 已更新） |
| 客户端 Web 构建 | 通过 |
| UI 端到端连接 | Playwright 驱动真实页面：登录 → 输入 9 位设备码 + 验证码 → 建会话 `ses-56afe062fcdb71cd`，进入「等待对方同意」 |

截图目录：`docs/impl/evidence/demo-screens/`（`client-*`、`admin-*`）。

## 四、仍未完成（不粉饰）

- 桌面端 Flutter 窗口需要 Xcode，本机只有 CommandLineTools，Web 预览替代；采集/编码/输入注入在浏览器不可用（自检页已给出说明）。
- P2P 打洞、NAT 类型、硬件编解码仍未实现/未在本环境验证，诊断页如实标注「待桌面端」。
- 文件传输、协助码、账号体系（微信/邮箱）仍为原型占位，未接后端。
