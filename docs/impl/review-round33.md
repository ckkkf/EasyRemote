# 第三十三轮：多设备主控身份 bug 修复（子代理实测发现）

> 日期：2026-11-20

## 一、问题来源

Ralph 子代理在跑弱网矩阵时发现：同一手机号反复绑设备后，`/v1/relay/ticket`
连续 12 次返回 `700005 ADMIN_FORBIDDEN "session party credential required"`；
换一个全新手机号、同一套代码立刻跑通。

它把可疑代码定位到 `server/internal/gateway/handler.go`：

```go
ctrlDevices, _ := a.Device.List(ctx, acc.ID)
controllerID := ctrlDevices[0].ID   // ← 取列表第一台
```

## 二、根因

`controllerID` 取的是「账号下设备列表的第一台」，而列表按 `device_code` 升序
（内存实现排序、PG 也是 `ORDER BY device_code ASC`）。

所以只要账号绑过两台以上设备，`controllerID` 就未必是**本次真正发起请求**的那台。
接着 `/v1/relay/ticket` 用 `X-Device-Token` 校验调用方是否会话参与方，
自然对不上 → 403/700005。

这不是偶发：**任何绑过第二台设备的账号都会命中**。

## 三、修复

主控设备改由请求头里的设备凭证解析：

```go
func (a *App) deviceFromCredential(ctx, r, accountID) (string, error) {
    token := r.Header.Get("X-Device-Token")
    if token == "" {
        return "", errcode.New(errcode.AdminForbidden, "controller device credential required")
    }
    devices, _ := a.Device.List(ctx, accountID)
    for _, d := range devices {
        if a.Device.VerifyDeviceToken(ctx, d.ID, token) {
            return d.ID, nil
        }
    }
    return "", errcode.New(errcode.AdminForbidden, "controller device credential invalid")
}
```

配套改动：
- `er-client` 的 `request_connect` 改为携带 `X-Device-Token`
- 集成测试全部补上主控设备凭证
- **新增回归测试** `TestMultiDeviceControllerIdentity`：同账号绑两台设备 →
  用新设备凭证发起连接 → 断言票据可领、且 `controller_id` 就是新设备

## 四、验证

```
--- PASS: TestMultiDeviceControllerIdentity
ok  github.com/easyremote/server/tests/integration
```

| 项 | 结果 |
|---|---|
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过（含新增回归） |
| vet / gofmt | 无输出 |
| Rust | 134 passed |
| Flutter | 18 passed |

## 五、顺带确认的弱网结论（子代理另一条线）

| 丢包 | 送达 | 整帧丢失 | 解不出 |
|---|---|---|---|
| 0% | 300/300 | 0 | 0 |
| 5% | 284/300 | 16 | **89（31%）** |
| 15% | 260/300 | 40 | **165（63%）** |
| 5%（720p，2 分片/帧） | 135/150 | 15（10%） | 66 |

两个结论：
1. **分片放大是真的**：720p 数据报丢 4.8% → 整帧丢 10.0%，与 `1-(1-p)^2=9.4%` 吻合
2. **比丢帧更致命的是参考链断裂**：5% 丢包只有 16 帧没到，却有 89 帧解不出——
   P 帧依赖前帧，而 GOP=60 才来一个 IDR

## 六、仍未做

| 缺口 | 说明 |
|---|---|
| ABR 从未被触发 | 数据面没有把丢包/RTT 回传给 `AbrController`，只有离线曲线 |
| 丢包触发关键帧 | 接收侧检测到空洞应发 KEYFRAME_REQUEST，未实现 |
| FEC / 分片重传 | 1080p 每帧 6–9 分片，5% 丢包会放大到 30%+ |
