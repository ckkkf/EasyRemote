# EasyRemote 文档索引

> 最后更新：2026-11-19 ｜ 状态：**需求已冻结，进入技术验证**

---

## 一、从这里开始

| 文档 | 说明 |
|---|---|
| [PRD v0.7](prd/easyremote-prd-v0.7.md) | **主文档**：产品定位、三期规划、功能规格、架构、里程碑 |
| [Spike W1 技术方案](spike/w1-plan.md) | **下一步要做的事**：采集→编码→渲染本机回环验证 |

## 二、规格（开发依据）

| 文档 | 内容 | 用途 |
|---|---|---|
| [错误码表](spec/error-codes.md) | 70+ 条码，8 个责任段 | 三端常量文件、客服排查 |
| [协议草案](spec/protocol.md) | 三通道、30 条消息、输入编码 | 联调依据 |
| [页面清单](spec/pages.md) | 客户端 20 页 + 后台 14 页 | 设计与开发排期 |

## 三、竞品研究

| 文档 | 内容 |
|---|---|
| [UU 远程技术拆解](research/uu-remote-analysis.md) | 功能矩阵、性能宣称辨析、技术栈三档证据、竞品对比 |

## 四、历史版本（PRD 演进）

| 版本 | 状态 | 关键变化 |
|---|---|---|
| v0.1 | 历史 | 初版，含商业化设计（后被移除） |
| v0.2 | 历史 | 去商业化、加后台系统、传输改 QUIC、新增 Spike |
| v0.3 | 历史 | 技术栈定稿（Flutter/shadcn、Go、React） |
| v0.4 | **作废** | 误将后台后端改为 TS，已被 v0.5 回退 |
| v0.5 | 历史 | 修正 v0.4：后台 Go 后端 + TS 前端 |
| v0.6 | 历史 | 客户端改 Material 3、传输改 WebTransport、会话角色预留 |
| **v0.7** | **当前** | 差异化三主线（网络诊断/防睡眠/会话水印）、功能补全、需求冻结 |

## 五、已定稿的关键决策

| # | 议题 | 结论 |
|---|---|---|
| 1 | 客户端 UI | Flutter 内置 Material 3，`ColorScheme.fromSeed` 品牌色 |
| 2 | 客户端核心 | Rust（quinn → wtransport） |
| 3 | 传输层 | WebTransport over HTTP/3（datagram 传视频、stream 传控制/文件） |
| 4 | 服务端 | Go + PostgreSQL + Redis |
| 5 | 管理后台前端 | React 18 + TypeScript + Vite + Tailwind + shadcn/ui |
| 6 | 管理后台后端 | Go |
| 7 | Windows 最低版本 | Win10 |
| 8 | 商业化 | 本阶段不做 |
| 9 | 差异化主线 | 网络诊断 / 防睡眠 / 会话水印（不可砍） |
| 10 | 首个里程碑 | 3 周 Spike，先证明 1080p@60 / ≤40ms |

## 六、仍待决定

| # | 议题 | 影响面 |
|---|---|---|
| A1 | 品牌主色色值 | Material 色板、Tailwind 主题、官网、图标 |
| A2 | 产品中文名 | 官网、商店、安装包 |
| A3 | wtransport 是否进 Spike 专项验证 | Spike 范围 |
| A4 | 后台多租户概念 | 企业版权限模型 |
| A5 | macOS 签名公证预算 | V1 排期 |
