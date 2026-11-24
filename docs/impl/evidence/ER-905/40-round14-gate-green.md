# round 14：gate 全绿（ER-905）

## 监督指出的阻塞与处理

### 1) `attach_error_ui_test.dart` 的 deprecated/unused import

该文件原本是「伪 UI 测试」（读单例、自己调 Clipboard），且带着 `material.dart` 导入。
**已重写为纯 SessionVideo 生命周期测试**（不导入 material，不冒充页面测试）：

```
TestSessionVideoFailureMessageIsStable    PASS  failure.display 只含稳定文案，无 http/URL
TestSessionVideoTraceIdGoesToClipboard    PASS  trace_id 可写入剪贴板
TestSessionVideoCloseClearsFailureForRetry PASS  close 清失败态，重试真的再次 attach
```

页面级断言保留在 `session_page_attach_ui_test.dart`（pump SessionPage / tap trace copy /
tap reconnect / Completer 防并发），本轮复跑 4/4 PASS。

### 2) 契约生成物与 formatter 互相打架（gate 第 3、15 步）

根因：生成器输出多行 `const X({...})`，而 `dart format` 会把短参数列表**折叠**成单行，
于是「生成」与「格式化后磁盘」永久不一致 —— 契约漂移检查与 format 检查互相否定。

修复（改生成器，不动 gate/基线）：

- `scripts/gen-contract.mjs` 新增 `formatDart()`：把 Dart 产物先写临时文件、
  用 `dart format` 规范化后再作为输出内容；
- `outputs` 里 DART_OUT 直接使用格式化后的内容；
- `--check` 对**磁盘内容也跑一次** `formatDart` 后比较，比较的是语义而非排版；
- `dart` 路径可用 `DART_BIN` 覆盖。

结果：`node scripts/gen-contract.mjs --check` → `✓ 三端契约一致（73 个类型 / 70 条接口）`，
`dart format --set-exit-if-changed lib test` → `0 changed`。

## 监督要求的 5 条命令（真实输出）

```
$ cd client && dart format --output=none --set-exit-if-changed lib test
Formatted 98 files (0 changed) in 0.28 seconds.        FORMAT_EXIT=0

$ cd client && flutter analyze
138 issues found —— 全部 info（存量），0 error / 0 warning

$ cd client && flutter test --concurrency=1
00:36 +121: All tests passed!

$ bash scripts/mac/e2e_lifecycle.sh > docs/impl/evidence/ER-905/e2e-run.stdout.txt
E2E[A]_PASS / E2E[B]_PASS PROBE_PEER_CLOSED reason=local_disconnect / E2E[C]_PASS rejected / E2E_ALL_PASS
E2E_EXIT=0

$ bash scripts/gate.sh > docs/impl/evidence/ER-905/39-gate-round14.txt
GATE_EXIT=0    GATE: PASS    (20/20 步)
```

## 证据指针

- 本轮 gate：`39-gate-round14.txt`（**替代** 38-gate-round13，后者不再作为最终依据）
- E2E：`e2e-run.stdout.txt`
- 页面级 widget 测试：`client/test/session_page_attach_ui_test.dart`
- SessionVideo 生命周期：`client/test/attach_error_ui_test.dart`、`attach_error_structured_test.dart`

## 仍未关闭

- `ER-905-PROTO-01`（中继注册无 ACK，两处固定等待）
- `ER-905-SPEC-01`（GW-11 无未授权精确码）
- `ER-905-PROTO-02`（reason 缺 actor 前缀）
