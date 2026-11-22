# ER-000 证据：能力成熟度台账与未闭环能力禁用

- Goal: goal-794d4aad-c164-4101-8fc9-f5e65753073f
- 分支/基线: dev @ 2d0a991（工作区保留用户既有 AGENTS.md 修改与整改总纲）
- 日期: 2026-11-22
- 状态: 本卡验收通过（严格 gate PASS；全量 Flutter 仅剩 ER-003 归属的 ABR 失败）

## 1. 交付内容

| 文件 | 目的 |
|---|---|
| docs/spec/capabilities.yaml | 能力成熟度唯一来源（7 条能力、owner、缺口原因） |
| scripts/gen-capabilities.mjs | 生成三端常量；--check 进 gate |
| client/lib/capability_gen.dart | 生成物：Dart 台账 |
| server/internal/capability/capability_gen.go | 生成物：Go 台账 |
| web-admin/src/capability_gen.ts | 生成物：TS 台账 |
| client/lib/capabilities.dart | 能力门：disabled 永不开放；experimental 仅非发布构建；未知 key fail-closed |
| client/lib/capability_view.dart | 未开放能力的统一空态（原因 + 成熟度 + 收口卡） |
| client/lib/prefs_store_types.dart | allow_control 默认/缺字段/脏数据 = false |
| client/lib/pages/settings_page.dart | 隐私屏/防休眠/被控总开关改为只读「未接入」+ 台账展示 |
| client/lib/pages/device_list_page.dart | 删除登录自动开被控；被控开关与连接按钮走能力门 |
| client/lib/pages/connect_page.dart | 发布构建不渲染连接假入口 |
| client/lib/pages/session_page.dart | 去掉「链路=已授权」伪值，未接入字段如实标注 |
| client/lib/shell.dart | 文件传输入口按能力门动态出现；设置仍固定最后 |
| client/test/capability_gate_test.dart | 能力门 fail-closed 断言 |
| client/test/prefs_test.dart / widget_test.dart / settings_prefs_test.dart | 新默认值与只读 UI 的回归 |
| server/internal/capability/policy.go | 服务端只能关不能开；ER_CAPABILITY_DISABLE 仅做减法 |
| server/internal/capability/policy_test.go | 服务端 fail-closed 断言 |
| server/internal/appconfig/appconfig.go | /v1/config 下发只读 capabilities 布尔表 |
| web-admin/src/App.tsx | 配置中心增加只读能力台账 |
| docs/spec/contract.yaml | 仅 GW-18 注释；接口形状未变 |
| scripts/gate.sh | 增加 gen-capabilities --check |

## 2. 字段映射

- HTTP 请求/响应字段：N/A。本卡不新增/删除接口；GW-18 response 仍为 any（ER-011 定型）。
- `data.config.capabilities`：新增只读布尔表，key 由 docs/spec/capabilities.yaml 生成；服务端只能报告/关闭。
- `allow_control`：docs/spec/pages.md:145（默认关）；缺字段迁移为 false。

## 3. 验收断言 -> 测试 -> 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 发布构建禁用未闭环能力；disabled 任何构建不可用 | `TestIncompleteCapabilityIsDisabled`（Dart） | PASS |
| 未登记能力 fail-closed | `TestUnknownCapabilityFailsClosed`（Dart/Go） | PASS |
| 偏好不能解锁发布构建 | `TestPreferenceCannotUnlockExperimentalInRelease` | PASS |
| allow_control 默认/缺字段/脏数据为 false | `TestAllowControlDefaultsOff` | PASS |
| 服务端非 stable 能力不可承诺，ENABLE 不能开 | `TestIncompleteCapabilityIsDisabled`（Go） | PASS |
| 禁用列表只做减法 | `TestDisableListOnlySubtracts` | PASS |
| 设置页不渲染可切换的假开关 | `未接入能力不渲染可切换的开关` | PASS |

## 4. 命令与关键输出

```
$ node scripts/gen-capabilities.mjs --check
✔ 三端能力台账与 docs/spec/capabilities.yaml 一致

$ cd client && flutter test test/capability_gate_test.dart test/prefs_test.dart test/settings_prefs_test.dart test/widget_test.dart test/shell_nav_test.dart
00:02 +22: All tests passed!

$ cd server && go test ./internal/capability/ ./internal/appconfig/
ok  github.com/easyremote/server/internal/capability

$ cd web-admin && npx tsc --noEmit   # TSC_EXIT=0
$ cd web-admin && npm run build      # ✓ built in 558ms（构建产物已回退，避免提交 tsbuildinfo）

$ cd client && dart analyze lib/capability_view.dart lib/capabilities.dart lib/capability_gen.dart
No issues found!

$ bash scripts/gate.sh
GATE: PASS
```

完整日志：`baseline-flutter-test.log`、`flutter-test-final.log`、`go-capability-test.log`、`web-admin-tsc.log`、`gate.log`。

## 5. 已知限制（不计入本卡完成，但必须保留）

1. Flutter 全量：61 pass / 1 fail。失败为 `native_test.dart: ABR 决策可跨边界返回`，与改动前基线一致（见 baseline 日志），归 ER-003。
2. `host_link_test.dart` E2E 因缺 ER_GATEWAY_URL/ER_RELAY_ADDR skip，归 ER-002。
3. 本机无 cargo，Rust fmt/clippy/test 未运行，归 ER-001。
4. `flutter analyze` 存量 112 issues（审查快照 111）；本卡新增文件为 0 issue，剩余为存量 lint 债务，归 ER-001。
5. 发布构建隐藏 experimental 入口（远程控制/文件传输），开发构建保留以便继续整改；这是本阶段的预期行为。

## 6. 回滚

- 代码回滚：revert 本卡文件即可；`allow_control` 默认值不回滚到 true（安全红线）。
- 能力开关回滚：只能调整 `docs/spec/capabilities.yaml` 的 maturity 并重新生成；禁止运行时开关。
- 数据：`allow_control` 历史快照缺字段读为 false，不需要数据库迁移。
