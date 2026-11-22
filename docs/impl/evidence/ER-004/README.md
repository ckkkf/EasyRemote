# ER-004 证据：四类模式扫描器 + 基线

- 状态: 完成（`bash scripts/gate.sh` → GATE: PASS）
- 日期: 2026-11-22

## 1. 交付

| 文件 | 目的 |
|---|---|
| `scripts/check-forbidden.mjs` | 6 条规则：httpx-bypass、bare-map、hardcoded-endpoint、empty-catch、fixed-sleep、ffi-pointer-balance；JSON 基线；--report/--update-baseline |
| `scripts/tests/check-forbidden.test.mjs` | 7 条 fixture 自测（正/负样本、测试文件排除、指针净差、基线格式） |
| `scripts/gate-baseline.json` | 存量清单（规则:文件 → 计数），只减不增 |
| `scripts/gate-baseline.txt` | 删除（旧格式无法表达多规则） |
| `scripts/gate.mjs` | forbidden 步骤描述更新 |

## 2. 规则与适用范围

| 规则 | 范围 | 判定 |
|---|---|---|
| httpx-bypass | server/internal + cmd 非测试、排除 httpx.go | json.NewEncoder / http.Error / w.Write([]byte |
| bare-map | adminhttp / gateway 非测试 | map[string]any |
| hardcoded-endpoint | client/lib 的 Dart | 127.0.0.1 / localhost / 0.0.0.0 / 4-5 位端口 / http(s) / ws(s) |
| empty-catch | client/lib 的 Dart | `catch (...) {}`（含多行空块） |
| fixed-sleep | client/lib Dart + server/internal Go 非测试 | Future.delayed / time.Sleep |
| ffi-pointer-balance | client/lib Dart | toNativeUtf8 次数 − calloc.free 次数 > 0 |

> 纯注释行不参与（避免文档示例误报）；test/example/stub 不扫描。

## 3. 存量清单（基线 19 条组合）

| 规则 | 文件 | 计数 |
|---|---|---|
| bare-map | gateway/assist.go | 2 |
| bare-map | gateway/config.go | 2 |
| bare-map | gateway/email.go | 2 |
| bare-map | gateway/files.go | 8 |
| bare-map | gateway/handler.go | 11 |
| empty-catch | native.dart | 1 |
| empty-catch | prefs_store_web.dart | 1 |
| empty-catch | session_store_io.dart | 3 |
| empty-catch | session_store_web.dart | 2 |
| empty-catch | session_video_io.dart | 2 |
| ffi-pointer-balance | host_service_io.dart | 12 |
| ffi-pointer-balance | session_video_io.dart | 10 |
| fixed-sleep | diagnostics_page.dart | 1 |
| fixed-sleep | session_page.dart | 2 |
| hardcoded-endpoint | main.dart | 1 |
| hardcoded-endpoint | net_probe_io.dart | 1 |
| hardcoded-endpoint | device_list_page.dart | 3 |
| hardcoded-endpoint | self_test_page.dart | 2 |
| hardcoded-endpoint | session_page.dart | 2 |

清零归属：hardcoded -> ER-400；FFI 指针/空 catch -> ER-402；fixed-sleep -> ER-201/ER-202；bare-map -> ER-011/ER-012；empty-catch 存量 -> ER-402。

## 4. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| httpx 绕过被计入 | `TestHttpxBypassDetected` | PASS |
| 网关裸 map 被计入 | `TestBareMapDetected` | PASS |
| 硬编码命中且测试/注释排除 | `TestHardcodedEndpointDetectedAndTestsExcluded` | PASS |
| 空 catch 单行/多行命中，有语句不命中 | `TestEmptyCatchDetected` | PASS |
| 固定 sleep 命中且测试排除 | `TestFixedSleepDetected` | PASS |
| FFI 指针净差 | `TestFfiPointerBalance` | PASS |
| 基线 JSON 往返 + 报告模式 | `TestReportOnlyAndBaselineFormat` | PASS |

## 5. 新增即红真实证据

临时新增 `client/lib/red_probe.dart`（硬编码 + Future.delayed）→ `check-forbidden` exit 1：

```
EXIT=1
新增违规（迁移完成前不许再扩散）：
  hardcoded-endpoint:client/lib/red_probe.dart 新增 1 处（基线 0）
  fixed-sleep:client/lib/red_probe.dart 新增 1 处（基线 0）
```

探针已删除，默认检查恢复「无新增违规」。日志：`red-probe.log`。

## 6. 命令与结果

```
$ node --test scripts/tests/*.test.mjs
12 tests pass（含本卡 7 条）

$ node scripts/check-forbidden.mjs
无新增违规（存量 19 条规则/文件组合待清零）

$ bash scripts/gate.sh
  scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
GATE: PASS
```

## 7. 回滚

- 回滚扫描器/自测/基线即可恢复旧行为；不得删除规则来让新增违规变绿。
- 基线只能通过负责人批准的 `--update-baseline` 重写；任何“为了过闸门”的基线扩大都视为违规。
