# 第二十五轮：UI 走真实信令（设备码 + 验证码）

> 日期：2026-11-20 ｜ 目标轮次 16/20

## 一、从「进程内协调」改成「真设备码流程」

上一轮的自检页是进程内协调器：主控需要 host_id（进程内知识）。
本轮改成真实接口——**主控只要设备码 + 验证码**：

```
被控端 prepare → 显示 设备码 + 验证码
被控端 serve   → 连信令，等待 CONSENT_REQUIRED（阻塞，跑在后台 isolate）
主控   connect → 用设备码 + 验证码建会话 → 收帧
```

## 二、关键改动

| 层 | 改动 |
|---|---|
| Rust | `conductor` v2：`prepare_host` / `serve_host` / `connect_device` / `release_host` |
| C ABI | `er_host_prepare` / `er_host_serve` / `er_device_connect` / `er_host_release` |
| Dart | `ErHostLink.prepare/serve/connect/release` |
| UI | 自检页三个按钮：准备被控端 → 等待连接 → 用设备码连接 |

`serve` 是阻塞调用（等信令最长 30s），UI 里用 `Isolate.run` 放到后台，
否则会卡住界面——这一点在实现时特意处理了。

## 三、实测（Dart → FFI → 真实三服务）

```
00:00 +0: 设备码 + 验证码 的完整会话
00:04 +1: 错误验证码被拒绝
00:04 +2: All tests passed!
```

断言链：准备返回 9 位设备码与非空验证码 → 后台 isolate 等信令 →
主控用设备码+验证码连上 → 被控端收到 **5 帧**、分辨率 > 0。

第二条测试证明错误验证码会被服务端拒绝（不是本地拦截）。

## 四、验证

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | **18 passed**（含新的自检页测试） |
| 设备码流程端到端 | 通过（真实信令 + 真实中继 + 5 帧） |

## 五、现在的完整链路

```
UI 点击
  ↓
Dart FFI → Rust
  ↓
网关 HTTP（登录/绑设备/登记公钥/建会话/取票据/取公钥）
  ↓
信令 WebSocket（被控端收到 CONSENT_REQUIRED）
  ↓
中继 UDP（加密帧，5/5 送达）
  ↓
H.264 解码
```

这一整条现在都在 UI 上有一个按钮能触发。

## 六、下一步

1. 断线重连（PRD §5.1.3）
2. ICE 打洞（现在强制中继）
3. 硬件编解码与 40ms 达标
4. 输入事件回传
