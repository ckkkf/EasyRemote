# 第十九轮：Flutter 能发起真实会话

> 日期：2026-11-20 ｜ 目标轮次 10/20

## 一、目标

上一轮底层链路全通，但 Flutter 界面还是静态的。本轮把会话能力通过 FFI 暴露出去，
让 Dart 能触发一次完整会话并拿到结果。

## 二、实现

### Rust 侧：`er_client::pair::run_pair`

把「主控 + 被控」两条流程编排进一个函数：登录 → 绑设备 → 登记身份 → 建会话 →
授权 → 取票据/公钥 → 中继注册 → 认证握手 → 加密传帧 → 解密解码，
返回结构化 `PairReport`。

### FFI：`er_run_pair_session`

```c
char* er_run_pair_session(gateway_url, relay_addr, phone_a, phone_b, sms_code);
```

返回 JSON：

```json
{"ok":true,"session_id":"ses-...","encoded_bytes":77340,"sealed_bytes":77368,
 "width":1920,"height":1080,"keys_match":true,"handshake_verified":true,
 "plaintext_absent":true}
```

### Dart：`ErSessionRunner`

```dart
final r = ErSessionRunner.load().run(
  gatewayUrl: 'http://127.0.0.1:18099',
  relayAddr: '127.0.0.1:19121',
  phoneA: '13700009001',
  phoneB: '13700009002',
);
expect(r['handshake_verified'], isTrue);
```

## 三、验证

```
00:00 +0: 完整会话：登录→建会话→握手→加密→解码
00:01 +1: 扁平 JSON 解析正确
00:01 +2: All tests passed!
```

这条测试需要真实服务在跑，用环境变量控制：

```
ER_CORE_LIB=../core/target/debug/liber_ffi.dylib \
ER_GATEWAY_URL=http://127.0.0.1:18099 \
ER_RELAY_ADDR=127.0.0.1:19121 \
flutter test test/session_e2e_test.dart
```

服务不在时自动跳过，不阻塞常规测试。

断言了五件事：`ok`、`handshake_verified`、`keys_match`、`plaintext_absent`、
以及密文比明文长（AEAD 有开销）。

## 四、验证汇总

| 端 | 结果 |
|---|---|
| Rust | 118 passed |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| 集成 | 通过 |
| Flutter | **15 passed**（13 + 2 新增） |
| Dart→FFI→真实服务 | 通过 |

## 五、诚实说明边界

`run_pair` 是**单进程双端**编排，用途是自检与联调，不是生产形态。

生产上主控和被控是两台机器、两个账号，各自调各自的接口。
真正的生产路径已经存在（`er-client` 的每个方法都是真实接口调用），
只是还没有一个「单独跑一端」的可执行入口。

这一点的实际影响：界面上的「连接」按钮现在调的是自检入口，不是真实的对端连接。
下一轮应补 `run_controller_only` / `run_target_only` 两个方向，让 UI 能做真正的一端。

## 六、下一步

1. 拆分单端 API（controller / target 各自独立）
2. Flutter 会话页接入真实帧流（目前只拿到最终尺寸，没有连续帧）
3. 断线重连、ICE 打洞
4. 硬件编解码与 40ms 达标
