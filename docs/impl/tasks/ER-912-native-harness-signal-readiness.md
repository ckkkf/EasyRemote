# ER-912 · 原生桌面验收等待实际信令注册

成熟度：仅测试，无产品行为提升。ER-910 的 macOS 原生回归首次在授权/领票据之前超时（`host did not see request before cancellation`）。开发 stack 不注入客户端网关 Presence，`/v1/devices.online` 会把绑定表布尔值当在线；不能拿它表示主机 WebSocket 已注册。本卡只修验收脚本的前置条件，不改脚本超时、拉起顺序或业务断言。

## 字段映射和能力链

N/A：不修改业务接口。使用已存在、仅 ER_E2E_HARNESS 输出的 `GET /debug/presence` 中 `devices`（`server/cmd/stack/main.go:136–153`）。本地隔离栈中只有被控端接入 WS → 实时 Hub 注册计数 → 首次发起设备码申请 → 真信令、原生采集、首帧、断开。生产设置、PG、UI 不改。

## 验收

原生脚本必须在零注册时不发送首次申请；有至少一个注册后执行原有断言，须输出 `HOST_CANCEL_CLEARED`、`HOST_REJECTION_SENT`、`FFI_FIRST_FRAME`、`HOST_CLOSE_PROPAGATED`、`ER906_NATIVE_CROSS_PROCESS_PASS`。规则检查和严格闸门不降级。回滚仅还原脚本中就绪条件；不能宣称 Windows 双机验收。

## 实测结果

修前在首次申请阶段复现失败；修后 `python3 scripts/mac/e2e_desktop_entry.py` 通过，`FFI_FIRST_FRAME 1920x1080 bytes=8294400`、`ER906_NATIVE_CROSS_PROCESS_PASS`。只证明单机双进程的原生回归，不证明已发布产品或跨设备 GUI。开发栈本身仍使用原有在线回退逻辑。
