# 第十八轮：客户端走真实服务链路（端到端全通）

> 日期：2026-11-20 ｜ 目标轮次 9/20

## 一、补的是什么

上一轮的信任链在**服务端**是通的，但客户端握手仍用硬编码种子。本轮让客户端真的走一遍服务：

```
登录 → 绑设备 → 登记身份公钥 → 建会话 → 授权 → 取票据 → 取公钥
     → 中继注册 → 认证握手（用服务端下发的公钥验签）→ 加密传帧 → 解码
```

## 二、新增 `er-client`

| 方法 | 对应接口 |
|---|---|
| `send_sms` / `login` | `/v1/auth/sms`、`/v1/auth/login` |
| `bind_device` | `/v1/devices/bind` |
| `register_identity` | `/v1/devices/identity` |
| `rotate_code` | `/v1/devices/verify-code` |
| `request_connect` / `consent` | `/v1/connect/request`、`/v1/connect/consent` |
| `relay_ticket` | `/v1/relay/ticket` |
| `session_keys` | `/v1/connect/keys` |

出错时解析服务端的错误信封（`{code, message}`），不吞错误。

## 三、实测输出

```
bound: target=dev-98e94006338d72b5 ctrl=dev-1dd61eeaee50d3c7
identities registered
session ses-80718447d93ed7e6 requested
consent state authorized
public keys from server match registered identities
handshake verified against server-provided keys
captured 1920x1080, encoded 77340 bytes
decoded 1920x1080
FULL SESSION E2E PASSED (real gateway, real relay, authenticated E2E encryption)
```

这一跑里：

- **真实网关进程**（`cmd/gateway`，内存 store）
- **真实中继进程**（`cmd/relay`，UDP）
- 两台虚拟设备各自登录、绑设备、登记 Ed25519 公钥
- 用验证码建会话、被控端授权
- 从 `/v1/connect/keys` 取公钥，**断言与登记值一致**
- 用取回的公钥验签对端握手
- 采集真实屏幕 1080p → H.264 → 加密 → 中继 → 解密 → 解码回 1080p

关键断言有两处：
1. 服务端下发的公钥 == 客户端登记的公钥（信任链没断）
2. 对端握手的签名能被这把公钥验过（中继冒充不了）

## 四、验证

| 端 | 结果 |
|---|---|
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| vet | 无输出 |
| Rust | 118 passed |
| Flutter | 13 passed |
| **完整会话端到端** | **通过**（真网关 + 真中继 + 认证加密） |

## 五、还不是生产形态的地方

1. 网关用的是**内存 store**（没起 PG，Docker 仍不可用），所以每次重启数据清零
2. 只有**一条**连接，没有并发/重连压测
3. 客户端还没有把这条链路接到 Flutter 界面上——UI 仍只能看静态页
4. 中继强制走，没有 ICE 直连

## 六、下一步

1. 把 `er-client` 的会话能力通过 FFI 暴露给 Flutter，让界面能真正发起会话
2. 断线重连（PRD §5.1.3）
3. ICE 打洞
4. 硬件编解码与 40ms 达标
