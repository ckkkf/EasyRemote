# 第二十四轮：信令端到端跑通（两台"机器"的完整流程）

> 日期：2026-11-20 ｜ 目标轮次 15/20

## 一、上一轮定位的两个缺口，本轮全部补上

### 缺口 1：网关不通知信令层

`/v1/connect/request` 建完会话只写库，没有告诉信令 Hub。补法：

```go
// gateway.App 增加可选钩子
type ConsentNotifier interface {
    NotifyConsentRequired(sessionID, from, to string) error
}
```

一体化部署（stack）里由 `hubNotifier` 直接调用 Hub；
多进程部署时换成 Redis Pub/Sub（`signal.Bus` 已有该能力）。

### 缺口 2：Hub 里没有绑定

第一次调用报 `signal: session not bound` —— Hub 的绑定原本只在
**信令 WS 的 RequestConnect** 里创建，而主控走的是 HTTP，从没在 Hub 里登记。

补 `Hub.BindFromServer`：HTTP 层已验证过账号与设备身份，
允许服务端直接建立绑定，不再要求「双方都在长连接上」。

### 缺口 3：ReadTimeout 杀死长连接

`serve.Run` 的 `ReadTimeout: 15s` 会把已 hijack 的 WebSocket 一起切断。
新增 `Config.DisableReadTimeout`，stack 二进制开启。

## 二、端到端实测

```
target=dev-b530b126080f17eb code=961992800
controller created session=ses-bf66c69079f5816c
target learned session via signalling: ses-bf66c69079f5816c   ← 关键：从 WebSocket 收到
sent 5 frames, received 5 frames
frame 0: 1920x1080 67878 bytes
frame 1: 1920x1080 15082 bytes
frame 2: 1920x1080 15430 bytes
frame 3: 1920x1080 13531 bytes
frame 4: 1920x1080 14024 bytes
SIGNALLING E2E PASSED (target discovered session via WebSocket)
```

服务端日志印证了通知链路：

```
notify: session=ses-bf66c69079f5816c from=dev-... to=dev-... err=<nil>
```

**被控端不再由代码告知会话 id，而是自己从信令里收到。** 这是"两台机器"的关键一步。

## 三、验证

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| vet / gofmt | 无输出 |
| Flutter | 18 passed |
| **信令端到端** | **通过（5/5 帧）** |

## 四、这轮暴露的三个问题都值得记

1. **服务端各自有测试 ≠ 接起来能跑**：网关和信令各有单测，但中间没接线，端到端就是死路。
2. **静默失败最贵**：`err=signal: session not bound` 一开始被 `_ =` 吞掉，
   是我在 notifier 里加日志才看见。现在这行日志保留。
3. **同一个超时配置害两种场景**：`ReadTimeout` 对普通请求是保护，对 WebSocket 是杀手。

## 五、下一步

1. UI 接入真实信令（现在自检页还是进程内协调器）
2. 断线重连（PRD §5.1.3）
3. ICE 打洞（目前强制走中继）
4. 硬件编解码与 40ms 达标
