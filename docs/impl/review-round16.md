# 第十六轮：认证的密钥交换（中继冒充不了任何人）

> 日期：2026-11-20 ｜ 目标轮次 7/20

## 一、补的是哪个洞

上一轮的加密只防「偷看」：中继可以分别和两端协商密钥，自己充当对端，看到全部内容。这在密码学上叫**未经认证的密钥交换**——加密强度再高也没用，因为对面是假的。

## 二、方案：Ed25519 握手签名

```
每端生成临时 X25519 密钥
  ↓
用长期身份密钥（Ed25519）对 (session_id ‖ ephemeral_pub ‖ role) 签名
  ↓
把 [ephemeral ‖ signature] 发给对端
  ↓
对端用**已知的设备公钥**验签（不是信里带的公钥）
  ↓
验签通过才协商密钥
```

三个绑定缺一不可：

| 绑定对象 | 防的是 |
|---|---|
| session_id | 把 A 会话的握手搬到 B 会话重放 |
| ephemeral 公钥 | 中继替换临时公钥做中间人（**核心**） |
| role（operator/controlled） | 两端角色互换劫持 |

签名域前缀 `easyremote-handshake-v1` 防止把签名挪去做别的用途。

## 三、全链路实测

```
host fingerprint fe812c12f3ab4ce6ac5db69ac352f906
ctrl fingerprint 5c29b78f10a35a49a6231d08ee840a04
both handshake signatures verified
captured 1920x1080, encoded 76422 bytes
decrypted 76422 bytes over authenticated channel
decoded 1920x1080, 6220800 rgb bytes
AUTH E2E PASSED (signatures verified, payload encrypted)
```

## 四、攻击场景回归（8 条单测）

| 攻击 | 结果 |
|---|---|
| 攻击者用自己的密钥签名，声称是 A | `BadSignature` |
| **保留合法签名，替换临时公钥**（典型 MITM） | `BadSignature` |
| 把 A 会话的握手用到 B 会话 | `BadSignature` |
| 把 operator 的握手当 controlled 用 | `BadSignature` |
| 用任意设备的公钥验签 | `BadSignature` |
| 不同对端临时公钥 | 派生出不同密钥，互相解不开 |

**「替换临时公钥」这条是重点**：中继最容易想到的作弊方式就是转发别人的签名、换上自己的公钥。因为签名覆盖了公钥字节，这一步必然失败。

## 五、验证

| 端 | 结果 |
|---|---|
| Rust | **118 passed**（110 → 118） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| vet | 无输出 |
| Flutter | 13 passed |
| 认证加密全链路 | 通过（签名验证 + 密文传输 + 解码） |

## 六、还缺的一环

现在验签用的是**硬编码的种子**（`[7u8;32]` / `[8u8;32]`）。
生产上需要：
1. 设备首次注册时把身份公钥登记到服务端（PG 表加一列）
2. 建会话时网关把对端公钥指纹下发给两端
3. 客户端校验指纹与设备码绑定

否则「已知的设备公钥」从哪来仍然没有答案——这一步不做完，MITM 防护在真实部署里仍会退化成 TOFU（首次信任）。

## 七、下一步

1. 设备身份公钥登记（服务端 + 网关下发指纹）
2. ICE 打洞
3. 输入事件回传链路
4. 断线重连、弱网降级矩阵
