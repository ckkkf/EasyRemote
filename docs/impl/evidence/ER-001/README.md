# ER-001 证据：严格闸门覆盖全栈

- Goal: goal-794d4aad-c164-4101-8fc9-f5e65753073f
- 日期: 2026-11-22
- 状态: 完成；`bash scripts/gate.sh` → **GATE: PASS**

## 1. 交付

| 文件 | 目的 |
|---|---|
| scripts/gate.mjs | 18 步全栈步骤清单、按 stack 汇总、--report/--only/--list |
| scripts/gate.sh | 薄封装（exec node scripts/gate.mjs） |
| scripts/check-forbidden.mjs | 禁止模式扫描（httpx / 裸 map），基线只减不增 |
| scripts/tests/gate.test.mjs | 闸门自测：CI 对照、失败聚合、analyze 判定 |
| .github/workflows/ci.yml | 新增 Flutter job；Rust clippy -D warnings；web test；gen-capabilities；check-forbidden；check-version；信封严格测试 |
| web-admin/package.json / package-lock.json | 引入 vitest，`npm test` |
| web-admin/tests/gate.test.ts | 能力台账与后台展示同源测试 |
| client/lib/**、client/test/** | 机械格式化；删除 zz_* 调试测试；清零 analyze warning |
| core/Cargo.lock | 工作区版本 0.2.7 → 0.2.8（cargo 自动同步；原先过期） |

## 2. 步骤矩阵（18 步）

scripts 自测；contract 生成物/路由/禁止模式/版本；go gofmt/vet/test -race/信封；rust fmt/clippy(-D warnings)/test；flutter format/analyze(error+warning=0)/test；web tsc/test/build。

- 生成物（contract.dart、*_gen.dart）不参与 dart format 检查：内容由生成器决定，人肉格式化会让 --check 与生成器空白风格冲突。
- analyze 只把 error/warning 判红；info 计入存量债务（111 条），由后续 lint 收敛卡处理（AGENTS 9.4）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 闸门步骤与 CI 同源，覆盖每个栈 | `TestGateMatchesCIJobs` | PASS |
| 任一栈失败 → 闸门红且失败栈准确 | `TestGateFailsWhenAnyStackFails` | PASS |
| 步骤 id 唯一、字段完整 | `TestGateStepsHaveUniqueIds` | PASS |
| 报告模式不判红 | `TestReportModeOnlyReports` | PASS |
| analyze 允许 info、拒绝 warning/error | `TestAnalyzeRejectsWarningsButAllowsInfo` | PASS |
| 后台台账同源 | vitest 3 tests | PASS |

## 4. 真实红灯证据（不是只跑单测）

| 栈 | 方式 | 日志 |
|---|---|---|
| contract | ER-001 首轮真实失败（生成物被格式化） | `docs/impl/evidence/ER-001-gate-initial.log` |
| rust | ER-001 首轮真实失败（存量 lint + ABR 测试） | 同上 |
| flutter | 临时加入必失败用例 → gate --only=flutter-test 退出 1 | `red-probes/flutter.log` |
| go | 临时加入必失败测试 → gate --only=go-test 退出 1 | `red-probes/go.log` |
| web | 临时加入必失败用例 → gate --only=web-test 退出 1 | `red-probes/web.log` |

探针文件已删除（`git status` 无 red_probe）。

## 5. 最终命令输出

```
$ node --test scripts/tests/*.test.mjs
# 5 tests pass（gate-selftest.log）

$ cd client && flutter analyze
111 issues found（0 error / 0 warning；gate 判 PASS）

$ cd client && flutter test
+62: All tests passed!

$ cd web-admin && npm test -- --run
Test Files 1 passed / Tests 3 passed

$ bash scripts/gate.sh
  scripts   PASS
  contract  PASS
  go        PASS
  rust      PASS
  flutter   PASS
  web       PASS
GATE: PASS
```

## 6. 依赖倒置与归属

- ER-001 的“gate 全绿”依赖 ER-003（ABR 失败）与 ER-001a（Rust 存量 lint），因此本轮按依赖实际顺序执行了 ER-003、ER-001a 后回到 ER-001；三张卡证据分别归档。
- 未完成/风险：analyze info 111 条（单独收敛卡）；`er-ffi` 的 `er-features` 依赖未被使用（cargo 提示，未阻塞 clippy）；web vitest 新增 6 个 audit 告警（依赖审计归 ER-902）。
- 回滚：`git revert` 本卡文件；闸门回滚不得退化为 report 模式或删除步骤。格式化可单独 revert，不影响逻辑。
