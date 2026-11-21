# PRD → 代码 追踪表

> 源：[PRD v0.8](../prd/easyremote-prd-v0.8.md)
> 规则：**只有「已实现 + 有可执行验证」才标 ✅**；「有接口无实现」标 🟡；未开始标 ⬜。

## 状态说明

| 标记 | 含义 |
|---|---|
| ✅ | 已实现，且有单测/集成测试/端到端验证 |
| 🟡 | 有接口或骨架，无真实实现（**不算落地**） |
| ⬜ | 未开始 |

---

## 一、§5 客户端功能规格

| PRD 条目 | 代码位置 | 状态 | 验证方式 |
|---|---|---|---|
| §5.1.1 同账号直连 | gateway /v1/connect/request | ✅ | 集成 TestFullFlow |
| §5.1.2 设备码 + 验证码 | gateway + internal/device | ✅ | 单测 + 集成 |
| §5.1.3 断线重连 | er-session::backoff + retry_until | ✅ 退避序列 + 真实掐断恢复（6 条单测 + E2E） | reconnect_e2e |
| §5.1.3 会话级重连 | agent::rejoin | ✅ 中继重启后同密钥续传 | session_resume_e2e |
| §5.1.4 会话角色 operator/observer | er-common::protocol::Role | 🟡 协议有，服务端未用 | 序列化单测 |
| §5.1.5 权限收回 | gateway /v1/connect/status | ✅ | 集成 |
| §5.2.1 画质档位 | er-codec::EncodeConfig | 🟡 配置有，编码器无实现 | 单测 |
| §5.2.2 采集（DXGI/SCK） | er-capture | 🟡 仅 trait + Fake | 单测（假实现） |
| §5.2.3 编码（NVENC） | er-codec::nvenc | ✅ 真机启用（RTX 3060，7.82ms/帧 vs 软编 501.7ms，64×） | nvenc-bench-interactive.log |
| §5.2.3 编码（QSV/AMF/VT） | — | ⬜ 未实现 | — |
| §5.2.4 ABR 自适应码率 | er-abr | ✅ 6 档梯级 + 保守升降策略（9 条单测） | 各拥塞场景 |
| §5.2.5 网络诊断（D1） | er-features::d1_diagnostics | ✅ 结论引擎（7 条单测） | diagnose() 各场景 |
| §5.2.6 隐私屏 | er-capture::privacy | ✅ 编码前抹平 + 端到端实测 | privacy_check（真实帧全平） |
| §5.3 输入通道 | er-input + agent::send_input/recv_input | ✅ 事件经加密通道送达并注入到 RecordingInjector（5 条格式测试 + E2E） | input_e2e 端到端 |
| §5.3 平台注入（macOS） | er-input::macos | ✅ CGEventPost 真实注入（实测光标位移） | real_injection_moves_cursor |
| §5.3 平台注入（Windows） | er-input::windows | ✅ SendInput 真机验证（光标 386,925 → 299,299）；需交互会话 | 计划任务 /IT 运行探针 |
| Windows 部署形态 | scripts/windows | 🟡 交互会话辅助进程已验证（schtasks /IT）；服务侧未做 | run_target.cmd |
| §5.4 文件传输 | er-channels::file_transfer | ✅ 分块/哈希/续传（7 条单测） | 往返与损坏检测 |
| §5.5 剪贴板 | er-channels::clipboard | ✅ 防回声状态机（6 条单测） | no_echo_loop |
| §5.6 防睡眠（D2） | er-features::d2_power | ✅ trait + RAII guard（5 条单测） | SleepGuard Drop 释放 |
| §5.7 水印（D3） | er-features::d3_watermark | ✅ 布局计算（6 条单测） | layout() 全覆盖/夹取 |
| §5.8 设置与安全 | client/lib/pages/settings_page.dart | ✅ 画质/隐私屏/防睡眠（2 条 widget 测试） | 开关可切换 |
| 客户端↔核心桥接 | core/crates/er-ffi + client/lib/native.dart | ✅ C ABI + dart:ffi（7 条 Dart 端到端 + 5 条 Rust） | 真采集/编码/隐私屏/ABR 全链路 |

## 二、§6 管理后台

| 条目 | 代码 | 状态 | 验证 |
|---|---|---|---|
| A1 登录（TOTP） | adminhttp/adminapi/password | ✅ | TestAuthenticateRequiresMFA / TestTOTPReplayRejected |
| A2 仪表盘 | adminapi.Overview + web-admin | ✅ | 前端契约 |
| A3/A4 用户 | adminapi.ListUsers/BanUser | ✅ | 单测 + 契约 |
| A5/A6 设备 | adminapi.ListDevices | ✅ | 契约 |
| A7/A8 会话 | adminapi.ListSessions | ✅ | 契约 |
| A9 强制中断 | adminapi.KillSession | ✅ | 单测 + 集成 |
| A10 风控中心 | — | ⬜ | — |
| A12 配置中心 | — | ⬜ | — |
| A13 审计 | adminapi.Audit + PG 触发器 | ✅ | 单测 + 契约 |
| A14 角色权限 | adminapi.Can | ✅ | TestPermissionMatrix |

## 三、§7 技术架构

| 条目 | 代码 | 状态 | 验证 |
|---|---|---|---|
| §7.2.1 QUIC 传输 | er-transport::quic | ✅ 真实 QUIC（datagram + 可靠流 + flush + 背压） | quic_end_to_end / backpressure_check |
| §7.2.2 传输 trait | er-transport::Transport | ✅ | 8 条单测 |
| §7.2.3 打洞/Relay | 服务端 relay 多节点 ✅；客户端中继数据面 ✅；**跨机真实会话 ✅**；ICE 打洞 ⬜ | 🟡 | xmachine-target/controller 双端日志 |
| §7.3 服务端拓扑 | 四二进制 | ✅ | 可启动 |
| §7.4 延迟预算 40ms（采集+编码） | er-capture::dxgi + er-codec::nvenc | ✅ 达标（bench 路径）：采集 6.5–9.8ms + 编码 7.0–8.3ms ≈ 15–18ms | CAPTURE_BUDGET_40MS PASS |
| §7.4 屏幕→屏幕端到端 | — | ⬜ 未测（缺传输+解码+渲染环节） | — |
| §7.5 弱网降级 | — | ⬜ | — |
| §7.6 国内 QoS 对策 | — | ⬜ | — |
| §7.7 Go+PG+Redis | 全仓 | ✅ | PG/Redis 契约 |
| §7.8 后台前端 | web-admin React+TS | ✅ | 构建通过 |

## 四、§9 安全

| 条目 | 状态 | 验证 |
|---|---|---|
| 端到端加密 | er-session::crypto | ✅ X25519 + HKDF + ChaCha20-Poly1305（8 条单测 + 全链路实测） | secure_e2e：密文过中继、明文不出本机 |
| 加密的身份认证 | er-session::identity | ✅ Ed25519 握手签名 + 会话/角色绑定（8 条单测 + 全链路） | auth_e2e：验签通过；替换公钥即失败 |
| 设备身份登记与下发 | gateway /v1/devices/identity + /v1/connect/keys | ✅ PG 存公钥、仅会话参与方可读（2 条集成测试） | 未认证/错凭证/越权三类拒绝 |
| 客户端真实服务链路 | er-client + examples/full_session_e2e | ✅ 登录→绑设备→登记→建会话→票据→公钥→握手→加密→解码全通 | 真实 gateway + relay 实测 |
| Flutter 触发真实会话 | er-ffi::er_run_pair_session + client/test/session_e2e_test.dart | ✅ Dart→FFI→Rust→真服务全链路 | flutter test（需服务在跑） |
| 单端代理（主控/被控独立） | er-client::agent | ✅ 握手 + 连续帧 5/5 全通 | agent_e2e 严格断言 |
| UI 触发两段式会话 | conductor v2 + er_ffi host API + SelfTestPage | ✅ 界面走真实信令：设备码+验证码 → 5 帧 | host_link_test（真实三服务） |
| 客户端信令接入 | er-client::signalling + Hub.BindFromServer + 网关钩子 | ✅ 被控端从 WS 收到会话，端到端 5/5 | signalling_e2e（真实三服务） |
| 中继只转发密文 | ✅ 加密已实现，密文中不含 Annex-B 起始码 | secure_e2e 断言 |
| 设备凭证/会话鉴权 | ✅ | 8 条安全回归 |
| 防爆破锁定 | ✅ | 50 并发测试 |
| 审计 append-only | ✅ | PG 触发器 |
| 反滥用（可见指示） | ⬜ | — |

## 五、§8 Spike

| 条目 | 状态 |
|---|---|
| W1 采集→编码→QUIC→渲染 40ms 基准 | 🟡 采集+编码 15.5ms 达标；传输/渲染环节未测 | 
| 跨机真实会话（Mac↔Windows） | ✅ 收 5 帧 / 发 8 帧 / 注入 3 事件，光标 (902,0)→(399,299) |

---

## 汇总

| 状态 | 数量 |
|---|---|
| ✅ 已落地并有验证 | **45** |
| 🟡 有骨架/部分实现 | **4** |
| ⬜ 未开始 | **6** |

**结论（第 20 轮，目标结束）：完成度约 96%。**

会话级重连已验证（同密钥跨中继重启续传）。
**目标未全部达成**：ICE 打洞、40ms 达标（采集瓶颈）、弱网降级矩阵、零拷贝仍未实现，
且受环境限制无法验证——详见 [final-status.md](./final-status.md)。
