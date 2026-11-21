# 第二十六轮：输入事件回传（键鼠事件走加密通道）

> 日期：2026-11-20 ｜ 目标轮次 17/20

## 一、做了什么

把输入事件接进已有的加密通道，与视频帧混流：

```
主控 → 输入事件 → 加密 → 中继 → 被控端 → 解密 → 注入
```

### 线格式（er-input::wire）

紧凑二进制，跨端一致：

| 类型 | 布局 |
|---|---|
| Key | tag(1) + scancode(u16) + pressed(u8) |
| MouseMove | tag(2) + x(i32) + y(i32) |
| MouseButton | tag(3) + button(u8) + pressed(u8) |
| Wheel | tag(4) + dx(i32) + dy(i32) |

### 混流

加密载荷首字节标记消息类型：`1=视频`、`2=输入`。
被控端的 `recv_frames` 跳过输入消息，`recv_input` 跳过视频消息——
两条接收路径互不干扰。

## 二、实测

```
frames=2 inputs=3
  Key { scancode: 4, pressed: true }
  MouseMove { x: 640, y: 360 }
  MouseButton { button: Left, pressed: true }
INPUT E2E PASSED (video + input multiplexed on the encrypted channel)
```

断言：2 帧视频 + 3 个输入事件全部到达；`RecordingInjector` 记录到 3 条；
事件内容逐字段比对一致（不是"看起来差不多"）。

## 三、验证

| 端 | 结果 |
|---|---|
| Rust | **123 passed**（118 + 5 新增格式测试） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | 18 passed |
| 输入回传端到端 | 通过（真实三服务） |

## 四、必须说明的局限

**注入器目前只有 `RecordingInjector`（记录用），没有真正的平台注入。**

也就是说：事件能从主控正确传到被控端并解析，
但**没有真的移动鼠标或敲键盘**——真实注入需要 macOS `CGEventPost`（且需要辅助功能授权）
或 Windows `SendInput`，这部分没做。

所以这一轮的成果是「控制通道打通」，不是「能远程操作了」。

## 五、下一步

1. 真实平台注入（macOS CGEvent / Windows SendInput）+ 权限提示
2. 断线重连（PRD §5.1.3）
3. ICE 打洞
4. 硬件编解码与 40ms 达标
