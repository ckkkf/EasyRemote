# 第二十七轮：macOS 真实输入注入（能操作了）

> 日期：2026-11-20 ｜ 目标轮次 18/20

## 一、做了什么

上一轮的注入器只会记录。本轮补上 macOS 的真实注入：`er-input::macos::MacInjector`，
用 CoreGraphics `CGEventPost` 直接把事件送进系统。

### 两套码表必须显式映射

USB HID 扫描码与 macOS 虚拟键码是两套完全不同的编号：

| 键 | HID | macOS keycode |
|---|---|---|
| a | 0x04 | 0 |
| s | 0x16 | 1 |
| return | 0x28 | 36 |
| space | 0x2C | 49 |
| esc | 0x29 | 53 |

覆盖字母、数字、控制键、功能键、方向键、修饰键约 60 个映射。

### 事件类型

| 输入 | CGEvent |
|---|---|
| MouseMove | `CGEventCreateMouseEvent(kCGEventMouseMoved)` |
| 左键 | `kCGEventLeftMouseDown/Up` |
| 右键 | `kCGEventRightMouseDown/Up` |
| 中键 | `kCGEventOtherMouseDown/Up` |
| 滚轮 | `CGEventCreateScrollWheelEvent` |
| 键盘 | `CGEventCreateKeyboardEvent` |

## 二、验证（这次是真的动了鼠标）

```
cursor moved to CGPoint { x: 200.0, y: 200.0 }
test macos::tests::real_injection_moves_cursor ... ok
```

做法：读当前鼠标位置 → 注入移到 (200,200) → 再读一次 → 断言落在 (200,200)±5。

**这是本机真实光标位移，不是模拟。**

端到端也跑了一遍（`ER_REAL_INJECT=1`）：

```
frames=2 inputs=3
  Key { scancode: 4, pressed: true }
  MouseMove { x: 640, y: 360 }
  MouseButton { button: Left, pressed: true }
INPUT E2E PASSED
```

主控发出的事件经加密通道到达被控端，由 `MacInjector` 真正投递给系统。

## 三、授权边界（重要）

macOS 需要「辅助功能」授权才能注入。未授权时系统**静默丢弃**事件——
不会报错，只是没反应。

因此测试用「读回光标位置」来判断是否真的生效；没动就跳过并打印提示，
而不是假装成功。生产环境应显式检测授权并引导用户开启。

## 四、验证汇总

| 端 | 结果 |
|---|---|
| Rust | **128 passed**（123 + 5） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | 18 passed |
| **真实注入（本机光标位移）** | **通过** |
| 注入端到端 | 通过 |

## 五、还没做的

| 缺口 | 说明 |
|---|---|
| Windows 注入 | `SendInput` 未实现（本机无法验证） |
| 授权检测 | 应显式检测辅助功能授权并引导 |
| 断线重连 | PRD §5.1.3 |
| ICE 打洞 | 强制中继 |
| 硬件编解码 | 软件 95ms/帧 |
