# 第十五轮：端到端加密（中继只见密文）

> 日期：2026-11-20 ｜ 目标轮次 6/20

## 一、问题

上一轮打通数据面后，中继转发的是**明文 H.264**。这意味着任何控制中继节点的人都能看到用户屏幕内容——PRD §9 写的「中继只见密文」当时只是架构上的分离，不是密码学事实。

## 二、方案

```
X25519 临时密钥 → HKDF-SHA256 派生双向密钥 → ChaCha20-Poly1305 AEAD 逐包加密
```

三个关键设计：

| 设计 | 原因 |
|---|---|
| **双向独立密钥** | 两个方向共用一个 nonce 空间，counter 迟早撞车 |
| **nonce = 96 位单调计数器 + 接收侧重放检查** | 密文被录制后重放会被拒 |
| **AAD 绑定 session_id** | 防止密文被搬到另一个会话复用 |

公钥走中继明文交换，私钥永不出本机。

## 三、全链路实测

```
key exchange done (public keys only, secrets never left the machine)
source 1920x1080, encoded 76668 bytes
sealed 76668 -> 76696 bytes (+28 overhead)
decrypted 76668 bytes, identical to source stream
decoded 1920x1080, 6220800 rgb bytes
SECURE E2E PASSED (relay saw ciphertext only)
```

**开销 28 字节**（12 nonce + 16 Poly1305 tag），相对于 76 KB 的帧可以忽略。

另外断言了密文中**不包含 Annex-B 起始码**（`00 00 00 01`），证明中继上跑的不是裸 H.264。

## 四、加密模块单测（8 条）

| 用例 | 验证点 |
|---|---|
| roundtrip_both_directions | 双向都能解 |
| ciphertext_differs_from_plaintext | 明文不出现在密文中 |
| tampering_detected | 改一个 bit 就解密失败 |
| replay_rejected | 同一密文第二次被拒 |
| different_session_aad_breaks_decryption | AAD 不同则解不开 |
| truncated_rejected | 短包被拒 |
| keys_are_directional | 计数独立递增 |
| wrong_direction_key_fails | 发送方不能用自己 tx 密钥解包 |

**过程中修的一个 bug**：接收侧 `rx_max_nonce` 初值 0，导致第一个包（counter=0）被判定为重放，全部测试红。改成 `Option<u64>` 表示「还没收过」。

## 五、验证

| 端 | 结果 |
|---|---|
| Rust | **110 passed**（102 → 110） |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| vet | 无输出 |
| Flutter | 13 passed |
| 加密全链路 | 通过（1080p，密文过中继） |

## 六、还没做的

1. **ICE 打洞**：现在强制走中继，直连缺失（延迟与带宽成本都受影响）
2. **身份认证**：当前密钥协商无认证，理论上可被中继做中间人（需要把设备身份签进密钥交换，例如用设备凭证签名临时公钥）
3. 输入事件回传链路
4. 断线重连、弱网降级矩阵
5. 硬件编解码、40ms 达标

> 第 2 条是真实缺口：现在的加密只防「中继偷看」，不防「中继冒充」。下一轮应把临时公钥用设备密钥签名，中继就无法插在中间。
