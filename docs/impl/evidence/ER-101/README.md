# ER-101 证据：C18 本地授权 controller + 弹窗

- 状态: 完成（controller/UI/测试）；bash scripts/gate.sh → GATE: PASS

## 1. 交付

| 文件 | 目的 |
|---|---|
| client/lib/consent_controller.dart | 唯一决策源：pending 请求、30s 主动倒计时、accept/reject/timeout/superseded、防止跨 session 决策 |
| client/lib/consent_dialog.dart | C18 弹窗：控制者、剩余秒数、接受/拒绝；只渲染 controller |
| client/test/consent_controller_test.dart | 4 条状态机测试 |
| client/test/consent_dialog_test.dart | 2 条 widget 测试 |

## 2. 不变量与实现

- 单 pending：新请求到达时旧请求以 superseded 拒绝，不静默覆盖。
- 只有当前 pending 的 session_id 能决策；其他 id 返回 false 且不产生决策（防远程/旧会话注入）。
- Timer.periodic 主动推进；到期立即产生 accepted=false/reason=timeout 并清空 pending。
- dispose 取消 Timer 并关闭决策流。
- 弹窗不持有时钟或状态，只读 controller。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 本机接受 | TestConsentAccept | PASS |
| 本机拒绝 | TestConsentReject | PASS |
| 30 秒超时自动拒绝并清空 | TestConsentTimeout | PASS |
| 远程/旧会话不能决策；新请求 superseded | TestRemoteCannotDecide | PASS |
| 弹窗展示控制者/倒计时，按钮驱动决策 | 弹窗两条 widget 测试 | PASS |

## 4. 命令与输出

    $ cd client && flutter test test/consent_controller_test.dart test/consent_dialog_test.dart
    All tests passed!
    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

## 5. 边界 / 归属

- 尚未接入真实信令：ER-102 删除 host_stream 自动 consent，把 CONSENT_REQUIRED 投给 ConsentController，并把决策回传；在此之前 remote_host 能力仍 disabled，UI 不可达。
- 控制者展示名由调用方提供；ER-102 需从会话/设备信息生成脱敏标签，不得显示可伪造的自由文本。
- 本卡不包含会话中常驻控制条（ER-700）。

## 6. 回滚

- 回滚 controller/UI 会重新失去本地决策入口，属于安全红线退化，禁止。
- 后续接入时不得把决策来源改为网络消息；只能由本机 UI 调用 accept/reject。
