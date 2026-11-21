# 第十二轮：输入注入 · ABR · 隐私屏 · Flutter 客户端

> 日期：2026-11-20 ｜ 目标轮次 3/20

## 一、输入注入（PRD §5.3）

新建 `er-input`，三层结构：

| 层 | 内容 |
|---|---|
| 事件模型 | `Key{scancode, pressed}` / `MouseMove` / `MouseButton` / `Wheel` |
| 映射 | 键名 → **USB HID Usage ID**（约 70 个常用键） |
| 注入 | `Injector` trait，平台实现负责真正发给系统 |

**为什么用扫描码不用字符**：被控端可能是不同键盘布局，传字符会出现「我按 A 他那边出 Q」。扫描码是物理位置，语义稳定。

另外两件事：
- 坐标用归一化（0..65535）传输，落地时按目标分辨率还原，解决两端分辨率不同的问题
- `RecordingInjector` 的 `live=false` 实现「观察者模式」——旁路观看时不注入任何输入

8 条单测覆盖：大小写映射、修饰键识别、观察者不注入、权限拒绝、坐标往返精度、边界判定。

## 二、ABR 自适应码率（PRD §5.2.4）

`er-abr`：6 档梯级 + 保守控制策略。

| 条件 | 动作 |
|---|---|
| 丢包 > 8% 或队列 > 200ms | 连降两档 |
| 丢包 2–8% 或 RTT > 250ms 或队列 > 80ms | 降一档 |
| 连续 3 周期健康 | 升一档 |
| 其它 | 保持 |

升档要连续 3 个周期健康——单周期健康就升会导致画面在好坏链路间反复抖动。

9 条单测覆盖：连降、降一档、RTT 触发、队列触发、升档需要连续、上下限夹取、梯级单调性。

**过程中修了一个真 bug**：初版 `since_change` 从 0 开始，首个反馈永远被间隔约束挡掉——突发丢包发生时第一次调整被忽略。改为初始即允许降档。

## 三、隐私屏（PRD §5.2.6）

`er-capture::privacy`：**编码前**把像素抹平，不是在显示层盖窗口（那样截屏仍能拿到内容）。

端到端实测：

```
non-zero bytes before=8289795 after=2073600
PRIVACY SCREEN PASSED
```

after 恰好等于像素数（1920×1080 = 2073600），说明只剩 alpha=255，内容已全部抹除。

## 四、Flutter 客户端（PRD §5）

**Flutter 3.50.0 已装好**，`client/` 工程可运行：

```
client/lib/
  main.dart           应用入口 + NavigationRail 主框架
  theme.dart          Material 3 + 品牌色 #1E4BDB
  models.dart         DeviceInfo / SessionState
  api.dart            GatewayClient（短信/登录/绑设备/发起连接/轮询状态）
  pages/
    login_page.dart       左侧蓝渐变品牌面板 + 右侧表单
    connect_page.dart     输入 9 位设备码
    device_list_page.dart 本机码 + 已绑定设备
    session_page.dart     等待授权 → 连接中 → 会话中
    diagnostics_page.dart D1 诊断展示
    settings_page.dart    画质/隐私屏/防睡眠
```

`flutter test` **6/6 通过**：设备码 3-3-3 格式化、设置页渲染、隐私屏开关可切换、诊断页指标、设备页展示。

API 客户端用 `MockClient` 注入测试，不依赖真实服务端。

## 五、验证汇总

| 端 | 结果 |
|---|---|
| Rust | **84 passed** |
| Go 单测 | 16 包全绿 |
| Go 竞态 | 16 包全绿 |
| Flutter | **6 passed** |
| 隐私屏端到端 | 通过（真实帧） |
| 真实采集 | 通过（1920×1080） |
| 真实 H.264 | 通过（编码 + 往返） |

## 六、环境状态

- **Docker 仍然起不来**（第三轮），PG/Redis 契约测试继续无法执行
- 磁盘 3.3 GB 可用，够但不宽裕

## 七、下一步

1. Rust 核心 ↔ Flutter 的 FFI 桥接（当前 UI 只能调 HTTP，不能真正建会话）
2. 断线重连（PRD §5.1.3）
3. 弱网降级矩阵（§7.5）
4. 硬件编解码 + 零拷贝采集（40ms 达标）
