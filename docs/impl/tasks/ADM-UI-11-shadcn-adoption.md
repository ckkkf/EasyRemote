# ADM-UI-11a/11b shadcn 组件真正落地（表 / 卡 / 按钮 / 图标 / 输入）

> 来源：用户反馈「管理端很丑，是不是没用 shadcn 组件」
> 事实核对（改前）：仓库里只有 8 个 primitive（button/input/card/badge/label/alert/dialog/textarea），
> 但页面层几乎没用它们——`App.tsx` 里 31 处 `className="btn"`、4 处手写 `<table className="tbl">`、
> 导航图标是 `▦ ☰ ▤` 这类字形、`styles.css` 285 行手写样式。**结论：确实没用起来。**

## 11a（已完成）

| 改动 | 说明 |
|---|---|
| `components/ui/table.tsx` | 官方 shadcn Table 源码（表头/行/单元格/横向滚动） |
| `components/ui/skeleton.tsx` | 官方 Skeleton（加载占位） |
| `components/ui/icon.tsx` | 内联 SVG 图标集（无新增依赖，路径同 lucide） |
| `components/business/data-table.tsx` | 改为基于 shadcn Table；`data-testid="data-table"` 作为测试钩子 |
| 业务 `Card` | 改为 shadcn Card/CardHeader/CardTitle/CardDescription/CardContent |
| 导航 + 顶栏 | 字形图标 → `<Icon>`；顶栏「刷新数据」→ shadcn Button |
| 测试/脚本 | `table.tbl` → `table[data-testid=data-table]`（11 个脚本、123 处） |

## 11b（已完成）

| 改动 | 说明 |
|---|---|
| 配置页 / 仪表盘两张表 / 能力成熟度表 | 手写 `<table class="tbl">` → shadcn Table |
| `.btn` 桥接样式 | 几何/状态对齐 shadcn Button（36px、8px 圆角、ghost/primary/danger、focus ring） |
| `input/select/textarea` | 36px 高、8px 圆角、聚焦环（与 shadcn Input 一致） |
| 导航项 | 36px 高、8px 圆角、surface-3 悬停、品牌浅底选中、16px 线性图标 |
| `styles.css` | 删除 `.tbl*`、`.card*` 手写规则（已无使用者） |

## 必须通过的断言

- [ ] `npx tsc --noEmit` 与 `npm test -- --run`：57/57。
- [ ] `TestSessionsE2E`：38 checks；`TestConfigAuditE2E`：26 checks。
- [ ] 六屏判据（列/行/导航项/指标卡）不变，视觉基线更新为 `ADM-UI-11b/screens/manifest.json`。
- [ ] `bash scripts/gate.sh`：`GATE: PASS`。

## 仍未做（诚实清单）

1. **没有引入 Recharts / RHF+Zod**（X5 仍 partial）：趋势图是 SVG polyline，表单是原生受控组件。
2. 页面里仍有 31 处 `.btn` 标记未替换成 `<Button>`（视觉已对齐，但语义仍是 `<button class="btn">`）。
3. 未引入 shadcn 的 Select / Tabs / Tooltip / Sonner / DropdownMenu / Sheet（需要额外 Radix 包）。
4. 没有暗色模式、没有 `data-slot` 约定、没有 shadcn CLI 的 registry 同步流程。

## 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/src/components scripts/admin-ui-baseline.py scripts/admin-ui-e2e-all.sh`；删除新增的 `components/ui/{table,skeleton,icon}.tsx` 与 `docs/impl/evidence/ADM-UI-11{b}/`。
