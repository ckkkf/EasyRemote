# 第二十二轮：单端代理接到 UI（会话自检页）

> 日期：2026-11-20 ｜ 目标轮次 13/20

## 一、本轮目标

上一轮单端代理 5/5 通过。本轮把它接到 Flutter：界面上能**启动被控端 → 显示设备码 → 用另一账号连接 → 看到收帧结果**。

## 二、两段式接口

| 层 | 接口 |
|---|---|
| Rust | `conductor::host_start` / `connect` / `host_stop` |
| C ABI | `er_host_start` / `er_host_connect` / `er_host_stop` |
| Dart | `ErHostLink.start/connect/stop` |
| UI | `SelfTestPage`（导航栏「自检」） |

```
start  → {"ok":true,"host_id":"host-...","device_code":"123456789"}
connect→ {"ok":true,"session_id":"ses-...","frames":5,"sent":5,
          "width":1920,"height":1080,"first_bytes":85545}
```

## 三、实测（Dart 直接调 FFI）

```
00:00 +0: 主机启动 → 主控连接 → 收到多帧
00:02 +1: 未知 host_id 返回错误而不是崩溃
00:02 +2: All tests passed!
```

断言：`frames == 5`、`sent == 5`、分辨率 > 0、首帧 > 1000 字节。

## 四、踩到的一个真实顺序约束

第一次跑失败在 `SESSION_AUTH_WINDOW_EXCEEDED: session not authorized`。

原因：`begin()` 内部会去取中继票据，而票据要求会话处于 `authorized` 状态。
我原来的顺序是「主控 begin → 被控授权」，票据在授权前就被请求了。

**正确顺序：建会话 → 被控授权 → 两端 begin → announce → finish。**

这类顺序约束在单测里很难发现，只有真跑服务才暴露。

## 五、UI 部分

新增「自检」页：输入网关/中继地址 → 启动被控端（显示 9 位设备码）→ 连接本机 → 展示发出/收到帧数与画面尺寸。

widget 测试覆盖：输入框与按钮渲染、未启动主机时「连接本机」按钮禁用。

## 六、验证

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | **18 passed**（15 + 3） |
| 两段式端到端 | 通过（5 帧，真实服务） |

## 七、诚实边界

自检页用的是**进程内协调器**（同一进程里既是主机又是主控），
因为它需要把两端的 session id 串起来，而真实产品靠信令服务完成这一步。

所以：**UI 能跑通真实链路，但还不是「两台机器」的场景**。
单端代理本身（`agent` 模块）已经是真的一端一个，缺的是信令侧的会话发现。

## 八、下一步

1. 信令侧：被控端订阅自己的会话请求（WebSocket 已有 Hub，需要客户端接上）
2. 断线重连（PRD §5.1.3）
3. ICE 打洞
4. 硬件编解码与 40ms 达标
