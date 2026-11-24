# ADM-UI-11a/11b 验证记录（shadcn 落地）

> 时间：2026-11-23 19:24-20:05（Asia/Shanghai）

## 改动

见 `docs/impl/tasks/ADM-UI-11-shadcn-adoption.md`（11a：Table/Skeleton/Icon + DataTable/Card/Button；11b：三张手写表 + 按钮/输入/导航几何 + 清理 285 行 CSS 中的废弃规则）。

## 断言 → 结果

| 断言 | 结果 |
|---|---|
| `npx tsc --noEmit` | PASS |
| `npm test -- --run` | 57/57 |
| `TestSessionsE2E` | PASS 38 checks |
| `TestConfigAuditE2E` | PASS 26 checks |
| 六屏判据（列/行/导航/指标卡） | 与 11a 一致：tables 2、filters 4、config 4/4/7 |
| 视觉基线 | 新基线 `evidence/ADM-UI-11b/screens/manifest.json` |
| `bash scripts/gate.sh` | `GATE: PASS` |

## 关键输出

```text
E2E OK (38 checks)   # sessions
E2E OK (26 checks)   # config + audit
BASELINE OK
GATE: PASS
```

## 未做 / 风险

- X5 仍 partial（Recharts/RHF+Zod 未引入）。
- 31 处 `.btn` 标记仍是原生 button + 桥接 CSS（视觉一致，语义未迁移）。
- shadcn 的 Select/Tabs/Tooltip/Sonner 等需要新增 Radix 依赖，未引入。
- 视觉基线是有意变更：三张表与按钮/输入/导航的几何都变了，已更新 manifest。

## 回滚

`git checkout --` App.tsx / styles.css / components / scripts；删除新增 primitive 与 `evidence/ADM-UI-11{b}/`。
