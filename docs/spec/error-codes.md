# EasyRemote 错误码表

> 配套文档：[PRD v0.7](../prd/easyremote-prd-v0.7.md) ｜ 版本 v1.0 ｜ 2026-11-19
> 状态：需求已冻结，本文为开发与联调依据。

---

## 1. 设计原则

1. **一个码只对应一个原因**。同一原因在不同入口出现，复用同一个码。
2. **码段即责任方**。看到码就知道该找谁：1xx 账号、2xx 网络、3xx 会话、4xx 媒体、5xx 文件、6xx 权限、7xx 服务端。
3. **用户提示与内部原因分离**。用户看到的是「对方拒绝了你」，日志里记的是 `200006 CONN_CONSENT_REJECTED`。
4. **不可猜测的信息一律模糊化**。设备不存在与设备未开启被控返回同一个码（200001），防枚举。
5. **码一旦发布不得复用或改语义**，只能新增或废弃（废弃的保留占位）。

## 2. 码段划分

| 段 | 范围 | 责任域 |
|---|---|---|
| 1 | 100001–199999 | 账号与鉴权 |
| 2 | 200001–299999 | 连接与网络 |
| 3 | 300001–399999 | 会话与控制 |
| 4 | 400001–499999 | 媒体与编解码 |
| 5 | 500001–599999 | 文件与剪贴板 |
| 6 | 600001–699999 | 权限与系统 |
| 7 | 700001–799999 | 服务端与后台 |
| 9 | 900001–999999 | 未知 / 兜底 |

## 3. 错误结构

```json
{
  "code": 200003,
  "name": "CONN_CAPTCHA_INVALID",
  "message": "验证码不正确",
  "detail": "attempt=3/5 remain=2",
  "retryable": true,
  "retry_after_ms": 0,
  "trace_id": "01HQ..."
}
```

| 字段 | 说明 |
|---|---|
| `code` | 6 位数字，见下表 |
| `name` | 大写下划线常量，代码里用这个判断分支，**不要用 code 数字** |
| `message` | 直接展示给用户的文案（已本地化） |
| `detail` | 仅进日志与诊断页，不展示给普通用户 |
| `retryable` | 是否值得重试 |
| `retry_after_ms` | 建议重试延迟（限流类错误必填） |
| `trace_id` | 全链路追踪 ID，客服排查用 |

---

## 4. 账号与鉴权（1xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 100001 | AUTH_INVALID_CREDENTIAL | 手机号或验证码错误 | 手机号或验证码不正确 | 不区分具体哪项错 |
| 100002 | AUTH_TOKEN_EXPIRED | 登录态过期 | 登录已过期，请重新登录 | 自动跳登录页 |
| 100003 | AUTH_TOKEN_REVOKED | 被踢下线 | 你的账号已在其他设备登录 | 提示后退出 |
| 100004 | AUTH_TOO_MANY_ATTEMPTS | 登录尝试过多 | 尝试次数过多，请 10 分钟后再试 | 锁定 10 分钟 |
| 100005 | AUTH_ACCOUNT_BANNED | 账号封禁 | 账号已被限制使用 | 展示申诉入口 |
| 100006 | AUTH_ACCOUNT_DELETED | 账号已注销 | 账号已注销 | 引导重新注册 |
| 100007 | AUTH_DEVICE_NOT_BOUND | 设备未绑定 | 请先登录后再使用 | 跳登录 |
| 100008 | AUTH_SMS_SEND_FAILED | 短信发送失败 | 验证码发送失败，请稍后重试 | 可重试 |
| 100009 | AUTH_SMS_RATE_LIMITED | 短信频控 | 发送过于频繁，请 60 秒后再试 | 倒计时 |
| 100010 | AUTH_CLIENT_VERSION_TOO_OLD | 版本过低 | 当前版本过低，请升级后使用 | 强引导升级 |
| 100011 | AUTH_REGION_RESTRICTED | 地区限制 | 当前地区暂不可用 | 仅日志 |
| 100012 | AUTH_CONCURRENT_LIMIT | 登录并发超限 | 登录设备过多 | 引导管理设备 |

## 5. 连接与网络（2xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 200001 | CONN_DEVICE_NOT_FOUND | 设备码不存在**或**未开启被控 | 设备不存在或未开启被控 | **合并返回，防枚举** |
| 200002 | CONN_DEVICE_OFFLINE | 设备离线 | 对方设备不在线 | 提供「远程开机」（V1） |
| 200003 | CONN_CAPTCHA_INVALID | 验证码错误 | 验证码不正确 | 计数，5 次锁定 |
| 200004 | CONN_CAPTCHA_EXPIRED | 验证码过期 | 验证码已过期，请在对方设备上查看新的验证码 | 提示刷新 |
| 200005 | CONN_CAPTCHA_LOCKED | 错误过多锁定 | 验证码错误次数过多，请 30 分钟后再试 | 锁定 30 分钟 |
| 200006 | CONN_CONSENT_REJECTED | 对方拒绝 | 对方拒绝了本次连接 | 24h 内重试上限 3 次 |
| 200007 | CONN_CONSENT_TIMEOUT | 对方 30s 无响应 | 对方未响应，连接已取消 | 可重试 |
| 200008 | CONN_DEVICE_BUSY | 设备正在被控制 | 该设备正在被其他设备控制 | 可「申请接管」V1 |
| 200009 | CONN_SIGNALING_UNREACHABLE | 信令不可达 | 无法连接服务器，请检查网络 | 重试 + 诊断 |
| 200010 | CONN_PUNCH_FAILED | 打洞失败（已切中继） | 已切换到中继模式，画质可能受影响 | **不打断用户** |
| 200011 | CONN_RELAY_UNAVAILABLE | 中继不可用 | 当前网络无法建立连接，请稍后重试 | 提示换网络 |
| 200012 | CONN_TIMEOUT | 建连超时 | 连接超时 | 重试 |
| 200013 | CONN_NETWORK_CHANGED | 网络切换 | 网络已切换，正在重连 | 自动重连 |
| 200014 | CONN_UDP_BLOCKED | UDP 被阻断 | 当前网络限制 UDP，已切换备用通道 | 自动 TCP 兜底 |
| 200015 | CONN_QOS_SUSPECTED | 疑似运营商限速 | 检测到网络被限速，建议错峰使用 | 诊断页明示 |
| 200016 | CONN_TLS_FAILED | 证书校验失败 | 连接不安全，已中止 | **不提供忽略选项** |
| 200017 | CONN_PROTOCOL_MISMATCH | 协议版本不兼容 | 双方版本不兼容，请升级客户端 | 提示最低版本 |
| 200018 | CONN_RATE_LIMITED | 建连频率受限 | 操作过于频繁，请稍后再试 | 限流 |
| 200019 | CONN_SELF_CONNECT | 连接到本机 | 不能连接到本机 | — |
| 200020 | CONN_PEER_UNREACHABLE | 对端不可达 | 无法找到对方设备 | 检查对方网络 |
| 200021 | CONN_ASSIST_CODE_INVALID | 协助码不存在或格式错误 | 协助码不正确，请核对后重试 | 不区分「不存在」与「已作废」 |
| 200022 | CONN_ASSIST_CODE_EXPIRED | 协助码过期（TTL 10 分钟） | 协助码已过期，请在对方设备上重新生成 | 提示重新生成 |
| 200023 | CONN_ASSIST_CODE_USED | 协助码已被兑换 | 协助码已被使用，请重新生成 | 一码一用 |

## 6. 会话与控制（3xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 300001 | SESSION_NOT_FOUND | 会话不存在 | 会话已结束 | 回首页 |
| 300002 | SESSION_EXPIRED | 会话过期 | 会话已过期 | 需重新连接 |
| 300003 | SESSION_TERMINATED_BY_PEER | 对方断开 | 对方已结束远程控制 | 回首页 |
| 300004 | SESSION_TERMINATED_BY_ADMIN | 管理员中断 | 本次会话已被中断 | 展示原因（后台填写） |
| 300005 | SESSION_ROLE_UNSUPPORTED | 角色不支持（MVP 收到 observer） | 当前版本不支持该连接方式 | 提示升级 |
| 300006 | SESSION_AUTH_WINDOW_EXCEEDED | 授权窗口超时（12h） | 本次授权已到期，请重新连接 | 需重新授权 |
| 300007 | SESSION_RECONNECT_EXHAUSTED | 重连次数耗尽 | 连接已断开，请重新连接 | 回首页 |
| 300008 | SESSION_KEY_NEGOTIATION_FAILED | 密钥协商失败 | 安全握手失败，连接已中止 | **不降级、不重试** |
| 300009 | SESSION_INPUT_REJECTED | 输入被拒绝 | 部分操作被对方设备拒绝 | 仅日志 |
| 300010 | SESSION_VIEW_ONLY | 只读模式 | 当前为仅观看模式，无法操作 | V2 |
| 300011 | SESSION_OBSERVER_LIMIT | 观看人数超限 | 观看人数已达上限 | V2 |
| 300012 | SESSION_KICKED_BY_HOST | 被被控端踢出 | 对方已断开你的连接 | 回首页 |

## 7. 媒体与编解码（4xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 400001 | MEDIA_CAPTURE_INIT_FAILED | 采集初始化失败 | 无法获取屏幕画面 | 回退 GDI |
| 400002 | MEDIA_CAPTURE_LOST | 采集中断（驱动崩溃/分辨率变更/切独占全屏） | 画面恢复中… | 自动重建采集 |
| 400003 | MEDIA_ENCODER_UNAVAILABLE | 无可用硬编 | 已切换到软件编码，性能可能下降 | 自动降级 |
| 400004 | MEDIA_ENCODER_INIT_FAILED | 硬编初始化失败 | 编码器初始化失败 | 回退软编 |
| 400005 | MEDIA_DECODER_INIT_FAILED | 解码失败 | 画面解码失败 | 请求关键帧 |
| 400006 | MEDIA_RESOLUTION_UNSUPPORTED | 分辨率超限 | 已降低分辨率 | 自动降档 |
| 400007 | MEDIA_BANDWIDTH_LOW | 带宽不足 | 网络带宽不足，已降低画质 | 自动降档 |
| 400008 | MEDIA_FRAME_DROP_HIGH | 丢帧严重 | 网络不稳定 | 诊断提示 |
| 400009 | MEDIA_DISPLAY_MODE_UNSUPPORTED | HDR/独占全屏不支持 | 当前显示模式可能影响画面 | 仅提示 |
| 400010 | MEDIA_HW_ACCEL_DISABLED | 硬解被禁用（驱动黑名单） | 已使用软件解码 | 维护黑名单 |
| 400011 | MEDIA_PRIVACY_SCREEN_ACTIVE | 隐私屏生效中 | 对方已开启隐私屏 | **主控端应无画面变化**，仅日志 |

## 8. 文件与剪贴板（5xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 500001 | FILE_NOT_FOUND | 文件不存在 | 文件不存在或已被移动 | — |
| 500002 | FILE_PERMISSION_DENIED | 无权限 | 没有访问权限 | 指明路径 |
| 500003 | FILE_DISK_FULL | 磁盘空间不足 | 目标磁盘空间不足，需要 X MB | 传前预检 |
| 500004 | FILE_TOO_LARGE | 超过单文件上限 | 文件过大 | — |
| 500005 | FILE_HASH_MISMATCH | 块校验失败 | 传输校验失败，正在重试 | 重传该块 |
| 500006 | FILE_TRANSFER_CANCELLED | 用户取消 | 传输已取消 | — |
| 500007 | FILE_PATH_TOO_LONG | 路径过长 | 文件路径过长 | — |
| 500008 | FILE_NAME_CONFLICT | 重名 | 文件已存在 | 弹窗三选一 |
| 500009 | FILE_READ_FAILED | 读取失败（被占用） | 文件被占用，无法读取 | 提示关闭程序 |
| 500010 | CLIPBOARD_TOO_LARGE | 剪贴板超限 | 内容过大，未同步 | 文本 1MB / 图 20MB |
| 500011 | CLIPBOARD_SENSITIVE_BLOCKED | 疑似敏感内容 | 检测到敏感内容，未自动同步 | 需手动确认 |
| 500012 | CLIPBOARD_FORMAT_UNSUPPORTED | 格式不支持 | 该格式暂不支持同步 | — |

## 9. 权限与系统（6xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 600001 | PERM_SCREEN_CAPTURE_DENIED | 屏幕录制权限未授予（mac） | 需要屏幕录制权限才能被控 | 引导跳系统设置 |
| 600002 | PERM_ACCESSIBILITY_DENIED | 辅助功能权限未授予（mac） | 需要辅助功能权限才能控制 | 引导 |
| 600003 | PERM_SERVICE_NOT_RUNNING | 后台服务未运行 | 后台服务未启动 | 引导重启服务 |
| 600004 | PERM_ELEVATION_REQUIRED | 需要管理员权限 | 该操作需要管理员权限 | 提供提权引导 |
| 600005 | PERM_ANTIVIRUS_BLOCKED | 被安全软件拦截 | 被安全软件拦截，请添加信任 | 提示具体软件名 |
| 600006 | PERM_SECURE_DESKTOP | 安全桌面不可注入（UAC 弹窗） | UAC 界面无法远程操作 | 说明限制 |
| 600007 | SYSTEM_SLEEPING | 系统即将睡眠 | — | **防睡眠功能应阻止此情况** |
| 600008 | SYSTEM_UPDATING | 系统更新中 | 对方系统正在更新 | 稍后重试 |
| 600009 | SYSTEM_LOCKED | 被控端已锁屏 | 对方设备已锁屏 | 提示解锁（V1 支持远程解锁） |
| 600010 | SYSTEM_USER_SWITCHED | 切换用户 | 对方已切换用户 | 会话保持但画面跟随 |

## 10. 服务端与后台（7xxxxx）

| 码 | 名称 | 触发条件 | 用户提示 | 处置 |
|---|---|---|---|---|
| 700001 | SRV_INTERNAL | 服务内部错误 | 服务异常，请稍后重试 | 带 trace_id |
| 700002 | SRV_DB_UNAVAILABLE | 数据库不可用 | 服务暂时不可用 | 降级 |
| 700003 | SRV_CACHE_UNAVAILABLE | 缓存不可用 | 服务暂时不可用 | 降级 |
| 700004 | SRV_MAINTENANCE | 维护中 | 系统维护中，预计 X 点恢复 | 公告 |
| 700005 | ADMIN_FORBIDDEN | 后台无权限 | 没有权限执行该操作 | — |
| 700006 | ADMIN_MFA_REQUIRED | 需要二次验证 | 请完成二次验证 | 跳 MFA |
| 700007 | ADMIN_SESSION_EXPIRED | 后台会话过期 | 登录已过期 | 重新登录 |
| 700008 | ADMIN_CONFIRM_REQUIRED | 需要二次确认 | 请确认本次操作 | 弹确认框 |
| 700009 | ADMIN_REASON_REQUIRED | 必填原因未填 | 请填写操作原因 | 阻止提交 |
| 700010 | ADMIN_CONFIG_INVALID | 配置值不合法 | 配置值不合法 | 校验白名单与取值域 |
| 700011 | SRV_ROUTE_NOT_FOUND | 路由不存在 | 请求的接口不存在 | 404，检查路径 |
| 700012 | SRV_METHOD_NOT_ALLOWED | 请求方法不被允许 | 请求方式不正确 | 405，检查 method 与 Allow 头 |

## 11. 兜底（9xxxxx）

| 码 | 名称 | 说明 |
|---|---|---|
| 999999 | UNKNOWN | 未归类异常。**出现即视为缺陷**，需补充归类 |

---

## 12. 使用规范

1. **分支判断用 `name`，不用 `code` 数字**。日志里两个都记。
2. 新增错误码必须同时更新本文档与三端常量文件（Rust / Go / Dart）。
3. CI 校验：三端常量文件的码集合必须完全一致，不一致则构建失败。
4. `200010 CONN_PUNCH_FAILED` 属于「内部状态」不是「错误」——**不弹窗**，只在诊断页可见。
5. 所有 `retryable=true` 的错误，客户端必须实现自动重试（带退避），不能只弹提示。

## 13. 常量文件位置（规划）

| 端 | 路径 |
|---|---|
| Rust core | `crates/common/src/errors.rs` |
| Go 服务端 | `internal/common/errcode/errcode.go` |
| Dart 客户端 | `lib/common/error_codes.dart` |
| 本文档 | `docs/spec/error-codes.md` |
