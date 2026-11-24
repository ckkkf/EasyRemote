# ER-906 · 桌面远控入口、退出和首帧

成熟度：beta；不提升能力等级。不修改接口、PRD、spec 或既有测试基线。

## 契约与能力
- GW-06 / GW-07 / GW-08 / GW-25：contract.yaml:366–378、748–766、846–852。只复用请求及状态；字段变更 N/A。
- 验证码错误以 error-codes.md:78–80 为准；同账号免临时密码不等于免本机授权（PRD §5.1.1–5.1.2）。
- GW-11 / GW-24：contract.yaml:385–387、780–785、867–873。保留既有认证和就绪状态。
- 链路：连接入口 → GW-06 → session / signal → HostService / ConsentController → 采集/中继 → er-ffi → SessionPage；断开由 GW-25 和真实 Navigator 路由回归守护。

## 诊断与最小设计
- Shell 的 maybePop 被会话 PopScope 拒绝，反向调用同一个断开入口。只在服务端确认关闭后开放会话本身的路由退出；提交与导航都幂等。
- 设备码表单无论是否免密都展示密码框；免密尝试优先，不把未知设备认作本账号。需要密码时再展示密码输入，后端仍核实账号及设备凭证。
- 被控核心把采集和编码器初始化放在信令前，媒体失败会让授权收件入口一起退出；本机接受前不启动媒体，媒体初始化失败时不进入数据面。对端关闭/本机失败不能留下虚假控制条。
- 会话页不能把已授权等同于已连接；首帧单独观察、失败可见且有边界。确认退出后，迟到配置、FFI attach/解码不得恢复画面。

## 验收与回滚
- 真 Navigator 上一次确认关闭、一次退出、一次提示；挂起期间不绕过。
- 免密发起不带验证码；跨账号未校验密码不能创建会话。
- 授权先于媒体启动；验证无帧诊断、迟到 attach 停止、有效帧渲染。
- 运行定向测试、对应栈测试、strict gate；跨进程和真实双桌面分别记录。无法验证则保留 beta。
- 独立回退本卡客户端和被控核心变更；若回归且无法即时恢复，禁用对应远控能力，不能绕过本机授权。

## 实测证据

| 验收 | 测试 |
|---|---|
| 确认关闭后真实路由只退出一次 | `TestConfirmedClosePopsRealNavigatorRouteOnlyOnce`（修前红、修后绿） |
| 免密优先且不跳过本机授权 | `TestDirectConnectRequestsNoPasswordAndRevealsFieldOnlyWhenNeeded`、`TestSameAccountDirectOmitsPasswordButNotConsent`、`TestCrossAccountDirectDoesNotCreateSessionWithoutPassword` |
| HELLO ACK 到达前收到的申请不丢 | `preserves_consent_delivered_during_hello`（修前红、修后绿） |
| 撤回清理、不会重复授权 | `remote_cancel_removes_pending_without_accepting`、`accepted_decision_is_not_presented_twice`、`TestCancelledNativeRequestCannotBeAccepted` |
| 首帧呈现、无帧提示、退出后不复活 | `TestActiveSessionRendersReceivedFrame`、`TestNoFirstFrameIsExplicitAndRecoverable`、`TestClosingDiscardsLateAttach` |
| 真实网关、信令、中继、双原生进程 | `scripts/mac/e2e_desktop_entry.py`：同账号免密 → 撤回 → 再请求并拒绝 → 再请求并接受 → 实际解码 1920×1080 RGBA → 主控断开、被控收敛 |

复现原生联调：`(cd core && cargo build -p er-ffi) && python3 scripts/mac/e2e_desktop_entry.py`。需要 macOS 有可采集显示器和录屏权限；不保存画面或凭证。浏览器/桌面 widget 测试、原生库双进程联调均不等价于两台机器上实际安装桌面客户端验收。当前设备缺完整 Xcode（`xcode-select` 仅 CommandLineTools），`flutter build macos --debug` 无法出包；未测 Windows ↔ macOS 和生产 WSS 部署。保留 beta。

另外两项未在本卡关闭：生产原生外部纹理改造、GW-06 尚无会话 ID 时取消在途请求的闭环。本卡不声称替换整个媒体/传输架构。

严格闸门：`bash scripts/gate.sh` → `GATE: PASS`；Flutter 127 项通过。原生联调输出 `FFI_FIRST_FRAME 1920x1080 bytes=8294400`、`HOST_CLOSE_PROPAGATED`、`ER906_NATIVE_CROSS_PROCESS_PASS`。定向 Go race 测试两项通过，`cargo test -p er-client --lib` 41 项通过，`git diff --check` 通过。
