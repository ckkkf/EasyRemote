# ER-910 · 中继票据不得越过会话授权窗口

成熟度：beta。只修 GW-11 的签发资格和新票据的有效期，不改变捕获、输入、公开 API、迁移或错误码。

## 字段映射
- 请求：`session_id`，沿用 GW-11（`docs/spec/contract.yaml:780–785`）的既有内部解码形状，N/A，不新增字段。
- 响应：`ticket`、`self_device`（`docs/spec/contract.yaml:385–387`），N/A；错误沿用 GW-11 和 `docs/spec/error-codes.md:109` 的 `SESSION_AUTH_WINDOW_EXCEEDED`。
- PRD §5.1.3（`docs/prd/easyremote-prd-v0.8.md:354–360`）：重连不重新弹窗，但最长授权窗口 12h。authorized 与 active 均仅在有效窗口内签发（active 不构成永久重连授权）。本卡不强制拆除既有数据面；到期主动收尾仍是独立任务。

## 能力链
桌面主控/被控 → GW-11 → 会话成员设备凭证 → session.Service 验证状态/授权 deadline → 轮换服务 → relaymgr 签发，期限不超过既有票据 TTL 与授权 deadline → relay Verify。错误沿用完整信封和会话错误记录。信令/UI/OS 采集未改。

## 验收
- `TestRelayTicketGateRejectsInvalidStateAndExpiredAuthorization`：真 HTTP 路由拒绝 requested、closed、缺失期限、已到期的 authorized/active、异设备访问；拒绝不轮换。
- `TestRelayTicketExpiresWithinAuthorizationWindow`：有效 authorized 可初领，active 可换领；票据截止不超过授权或原 TTL，验证边界。
- `TestRelayTicketDeniesExpiryDuringEpochRotation`：在签发前窗口耗尽不返回票据。
- Go race 全量、严格 gate、桌面原生回归。独立回滚本卡新增方法和 GW-11 调用，无数据迁移。

## 本轮验证

修前 GW-11 对缺截止时间、到期 authorized 和到期 active 返回 200；新回归稳定触发。修后 Go race 定向覆盖网关、会话、转发器和票据服务，严格 gate 通过（`GATE: PASS`）。macOS 原生首帧与断开参考 ER-912。GW-11 拒签不自动清理已有数据面；旧会话需要现有结束流程，主动到期回收不在本卡。升级时 Relay 校验端与网关应一同升级；旧版 Relay 在票据到期秒仍会接受，不承诺撤销升级前已签发的存量票据。
