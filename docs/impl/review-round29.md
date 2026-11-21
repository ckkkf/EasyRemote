# 第二十九轮：Windows 真机验证（注入通过）

> 日期：2026-11-20 ｜ 追加轮次

## 一、环境

通过 frp（`<frp-host>:<port>` → 本机 22）SSH 接入用户的 Windows 笔记本：

| 项 | 值 |
|---|---|
| 主机 | WIN-VERIFY-PC |
| 系统 | Windows 11 专业工作站版 build 26200 |
| 用户 | Administrator |
| 显卡 | **NVIDIA RTX 3060 Laptop** + AMD Radeon 集显 |
| 磁盘可用 | 47 GB |

Rust 工具链原本缺失，用 **rsproxy.cn 国内镜像**安装（未使用任何代理/VPN）：
`cargo 1.100.0` / `rustc 1.100.0`。

## 二、Windows 侧编译与单测：通过

因为没有 github.com 访问、crates.io 索引拉取很慢，我把 `er-input` 抽成**零依赖独立包**（9.9 KB）上传编译：

```
running 17 tests
test result: ok. 17 passed; 0 failed

其中包含：
  windows::tests::letters_map_to_distinct_vk   ok
  windows::tests::known_keys_map_correctly     ok
  windows::tests::unknown_hid_rejected         ok
  windows::tests::observer_mode_does_not_post  ok
```

说明 HID→VK 映射表在真实 Windows 上类型正确、逻辑正确。

## 三、真实注入：先失败，后通过（关键发现）

### 第一次：SSH 直接运行 → 失败

```
screen=1024x768              ← 不是真实分辨率
cursor_before=None          ← GetCursorPos 拿不到
inject_result=Err(PermissionDenied)
moved_to_target=false
RESULT: FAIL
```

### 原因：Windows 会话隔离

SSH 登录的进程跑在 **Session 0**（服务会话），那里没有交互桌面：
- `GetCursorPos` 失败
- `SendInput` 返回 0（被系统拒绝）
- 屏幕尺寸是 Session 0 的虚拟桌面（1024×768）

这是 Windows 的安全设计，不是代码 bug。

### 第二次：计划任务 /IT 在交互会话运行 → 通过

```
screen=1646x926             ← 真实桌面分辨率
cursor_before=Some((386, 925))
inject_result=Ok(())
cursor_after=Some((299, 299))
moved_to_target=true
RESULT: PASS
```

做法：`schtasks /create /ru Administrator /it` 让任务在用户交互会话里跑，
再把输出重定向到文件读回来。

## 四、对本产品的直接含义

**Windows 被控端必须以「服务 + 交互会话辅助进程」的形态部署**，
不能只在服务里调 SendInput——否则注入会被系统静默拒绝。

这解释了为什么 ToDesk、UU远程 这类产品都装两个组件：一个开机自启的服务
负责常驻与权限，一个运行在用户会话里的进程负责采集与注入。
我们的 `windows.rs` 实现是对的，但**部署形态**必须遵循这条约束。

## 五、验证汇总

| 项 | 结果 |
|---|---|
| Windows SSH 接入 | ✅ |
| Rust 工具链安装（国内镜像） | ✅ |
| er-input 在 Windows 编译 | ✅ |
| er-input 单测（17 条） | ✅ 全过 |
| **SendInput 真实注入** | **✅ 光标从 (386,925) 移动到 (299,299)** |
| NVENC 硬件编码 | ⬜ 未验证（有 RTX 3060，待接实现） |
| 完整 workspace 编译 | ⬜ 未完成（crates.io 索引拉取受阻） |

## 六、下一步

1. 打通 Windows 侧 cargo 镜像（或离线 vendor），跑完整 workspace
2. 接 NVENC（有 RTX 3060，可以做真实硬件编码并对比软件编码的延迟）
3. 用这台机器验证「Mac ↔ Windows 跨机真实会话」
