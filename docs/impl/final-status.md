# EasyRemote 交付状态（20 轮目标结束）

> **2026-11-21 更新（第四十三轮）**：反向跨机（Windows 主控 → macOS 被控）**已真机跑通**——
> 新增 `mac_target`（macOS 被控端）、`win_controller`（Windows 主控端）、像素↔points 坐标换算、
> 以及 `scripts/mac/step4_win2mac.sh` 编排。2026-11-21 10:49 跨机 PASS：Windows 发 3 个鼠标事件
> → macOS 真注入落点 (400,300) 误差 0px；macOS 真实 1920x1080 帧回到 Windows 并解码成功。
> 见 `docs/impl/review-round43.md`、`docs/impl/evidence/win2mac-2026-11-20/`。
> 边界：Windows 主控端跑在 SSH 会话（Session 0），回显画面走合成帧回退，输入与被控画面为真实。

> **2026-11-20 更新（第三十轮）**：本文下面「硬件编解码 ❌ 未实现」「Windows 输入注入 ❌ 未实现」
> 两条已被 `docs/impl/review-round30.md` 推翻——NVENC 已在 RTX 3060 真机跑通（7.8ms/帧 @2880×1620），
> Windows 注入已在交互会话跑通（光标 (902,0) → (399,299)），跨机真实会话全链路跑通。
> Windows 侧 `cargo build` / `cargo test` 也已全绿（135 passed / 0 failed）。
> 仍成立的部分：ICE 未做、采集仍是 xcap 全帧拷贝、NVENC 未零拷贝。


> 日期：2026-11-20 ｜ 目标轮次 20/20

## 一、这一轮做了什么：会话级重连

上一轮验证的是**链路层**重连（重新注册）。这一轮验证**会话层**：
中继重启后，加密会话（session_id + 密钥 + nonce 序列）继续有效。

新增 `ControllerAgent::rejoin` / `TargetAgent::rejoin`：只补端位，不重做握手。

### 实测

```
session ses-d5948fc320d567a9 established
before restart: sent 2 received 2
session resumed after 0 retries (same keys, same session id)
after resume: sent 2 received 2
SESSION RESUME PASSED (crypto session survived relay restart)
```

流程：建会话 → 收 2 帧 → **杀掉中继进程** → 重启 → 用同一套密钥续传 →
探针帧通过 → 再连续收 2 帧（证明不是"只通一帧"）。

## 二、最终验证（全绿）

| 端 | 结果 |
|---|---|
| Rust | **134 passed** |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成测试 | 通过 |
| vet / gofmt | 无输出 |
| Flutter | **18 passed** |

## 三、端到端验证清单（都是真跑，不是 mock）

| 能力 | 验证方式 |
|---|---|
| 真实屏幕采集 | 抓到 1920×1080 帧 |
| 真实 H.264 编解码 | 编码 963B / 往返一致 |
| 真实 QUIC 传输 | datagram + 可靠流往返 |
| 中继数据面 | 采集→编码→中继→解码 5/5 |
| 端到端加密 | 密文过中继，明文不出本机 |
| 握手认证 | Ed25519 验签；替换公钥即失败 |
| 设备公钥登记 | 服务端存储 + 会话方读取 + 越权拒绝 |
| 信令 | 被控端从 WS 自行发现会话 |
| 输入回传 | 事件经加密通道送达 |
| **真实输入注入** | **本机光标真的移动到 (200,200)** |
| 断线重连 | 真杀中继进程后恢复 |
| 会话级重连 | 杀中继后同密钥续传 |
| UI 全链路 | Dart→FFI→Rust→真服务，5 帧 |

## 四、明确未完成的部分（不粉饰）

| 缺口 | 状态 | 原因 |
|---|---|---|
| ICE 打洞 | ❌ 未实现 | 需要 STUN/TURN 环境与 NAT 场景，本地无法验证 |
| 硬件编解码 | ❌ 未实现 | VideoToolbox/NVENC 需要平台 SDK；当前 OpenH264 软件编码 95ms/帧，未达 40ms |
| Windows 输入注入 | ❌ 未实现 | 无 Windows 机器，`SendInput` 无法验证 |
| 弱网降级矩阵 | ❌ 未实现 | 需要 tc/netem 类网络损伤环境 |
| 文件传输/剪贴板接入会话 | 🟡 逻辑完成但未接进 agent 会话 |
| D1/D2/D3 接入 UI | 🟡 逻辑完成，UI 只有静态展示 |
| W1 40ms 基准 | ❌ 未达标 | 实测采集 209ms + 编码 95ms |

## 五、结论

按 PRD v0.8 全量落地的目标**尚未完成**。
已完成的是：服务端全部核心能力、客户端数据面闭环、安全（加密+认证+信任链）、
输入通道、重连、Flutter 骨架与 UI 触发链路；**缺的是性能达标（硬编解码/零拷贝）
和网络拓扑（ICE）这两块**，它们都受当前环境限制无法验证。

追踪表：约 **96%**，剩余项均为明确标注的未实现，不是"已规划"。