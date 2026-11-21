# 第十三轮：Rust 核心 ↔ Flutter FFI 桥接

> 日期：2026-11-20 ｜ 目标轮次 4/20

## 一、为什么这一步关键

上一轮 Flutter 只能调 HTTP 接口，**没法真正采帧、编码、显示远端画面**。
这一轮把 Rust 核心通过 C ABI 暴露出去，Dart 用 `dart:ffi` 直接调用。

产物：`core/target/debug/liber_ffi.dylib`（1.6 MB）

## 二、C ABI 设计

| 约束 | 做法 |
|---|---|
| 不 panic | 每个 `extern "C"` 函数体包 `catch_unwind`，panic 返回 `ER_ERR_PANIC(-99)` |
| 错误可读 | 负数返回码 + `er_last_error()` 取文本 |
| 内存归属 | 库分配的字符串由 `er_free_string` 释放；帧数据写进调用方 buffer，跨边界不转移所有权 |
| 句柄 | 不透明 `*mut Session`，Dart 只持有 `Pointer<Void>` |
| 空指针 | 每个入口先判空，返回 `ER_ERR_NULL` |

导出函数：

```
er_version / er_session_new / er_session_free
er_screen_size / er_frame_size / er_grab_frame
er_encode_last_frame / er_set_privacy / er_last_frame_uniform
er_abr_update / er_last_error / er_free_string / er_privacy_scrub
```

## 三、跨语言端到端验证（7 条 Dart 测试，全部通过）

```
版本号可读                        ✅
创建会话并读取屏幕尺寸             ✅
抓帧字节数与尺寸一致               ✅  (w*h*4，且非全零)
隐私屏开启后整帧被抹平             ✅  (uniform=true；关掉后恢复 false)
编码产出的 H.264 以 Annex-B 开头   ✅  (校验 00 00 01 / 00 00 00 01)
ABR 决策可跨边界返回               ✅  (拥塞时档位下降且 changed=true)
重复 close 安全                    ✅
```

这些不是 mock——**Dart 真的开了一个 Rust 会话，抓了你屏幕的 1080p 帧，编码成 H.264，再判断起始码**。

Rust 侧另有 5 条自检：隐私抹平、非法输入拒绝、空指针防护、字符串往返释放、会话生命周期。

## 四、无显示环境的兼容

`er_privacy_scrub` 是不依赖屏幕的纯函数（给定 buffer 直接抹平），
保证在无显示器 CI 环境里隐私屏逻辑仍可测。

## 五、验证汇总

| 端 | 结果 |
|---|---|
| Rust | **89 passed** |
| Go 单测 / 竞态 | 16 包全绿 / 16 包全绿 |
| vet | 无输出 |
| Flutter | **13 passed**（6 widget + 7 FFI） |

## 六、当前缺口

1. 会话链路未串通：信令/中继服务端已通，但客户端还没把「采集→编码→QUIC→对端」接起来
2. 硬件编解码、零拷贝采集（40ms 达标前提）
3. 断线重连、弱网降级矩阵
4. 端到端加密（PRD §9）

## 七、环境

Docker 仍不可用（第 4 轮），PG/Redis 契约测试继续欠账。
