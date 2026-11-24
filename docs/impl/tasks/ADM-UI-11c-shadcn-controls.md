# ADM-UI-11c 全站改用 shadcn 组件（Select / Switch / Input / Table / Button）

> 来源：用户要求「管理端前端都改成用 shadcn 的组件」
> 前置：ADM-UI-11a（Table/Card/Icon）、11b（手写表 + 按钮/输入几何）

## 目标

把页面里剩下的原生控件全部换成 shadcn：

| 控件 | 之前 | 现在 |
|---|---|---|
| 下拉（16 处） | 原生 `<select>` + `.tbl` 风格 | shadcn Select（Radix）+ `SimpleSelect` 薄封装 |
| 自动刷新（3 处） | `<label class="switch"><input type=checkbox>` | shadcn Switch（Radix） |
| 搜索/时间输入（14 处） | `input.search` / 原生 datetime-local | shadcn Input |
| 表格 | 手写 `<table class="tbl">` | shadcn Table（11a/11b 已完成） |
| 按钮 | `<button class="btn">` + 桥接 CSS | shadcn Button（顶栏）+ 桥接样式（其余待 11d） |

## 新增依赖（shadcn 官方依赖）

`@radix-ui/react-select`、`@radix-ui/react-switch`、`@radix-ui/react-separator`、`lucide-react`。

## 关键实现约束

1. **Radix Select 不接受空字符串 value**：空语义（全部/全局/未选择）用 `SENTINEL='__none__'`，在 `onValueChange` 里映射回 `''`。
2. **可点击目标**：shadcn Switch 默认 `h-5`（20px）低于 ADM-UI-10 的 24px 下限，改为 `h-6`。
3. **窄屏溢出**：顶栏 `flex-wrap` + `.top-actions` 允许换行 + `.shell/[data-card]` `min-width:0`。
4. **E2E 交互**：Radix Select 不是原生 select，新增 `scripts/admin_e2e.py` 的 `pick(page,label,value)`（等触发器可用 → 点开 → 点 `[role=option][data-value]`）；31 处 `select_option` 全部改写。
5. **行点击**：`<tr>` 命中测试会被 `<table>` 拦截，E2E 改为点第一个 `<td>`。

## 验收断言

- [ ] `npx tsc --noEmit` + `npm test -- --run`（57/57）。
- [ ] `bash scripts/admin-ui-e2e-all.sh`：`PASS: 14 / FAIL: 0`。
- [ ] `admin-ui-responsive-a11y.py`：`AUDIT OK`（4 档视口，无溢出、无 <24px 目标）。
- [ ] 六屏判据不变，视觉基线更新为 `evidence/ADM-UI-11c/screens/manifest.json`。
- [ ] `bash scripts/gate.sh`：`GATE: PASS`。

## 未做

- 31 处 `.btn` 标记仍是 `<button class="btn">`（视觉已对齐 shadcn Button，语义未迁移）。
- 未引入 shadcn 的 Tabs / Tooltip / DropdownMenu / Sheet / Sonner / Command。
- 图表仍是 SVG polyline（未引入 Recharts）。

## 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/src/components scripts`；删除 `components/ui/{select,switch,separator,simple-select}.tsx` 与 `scripts/admin_e2e.py`、`evidence/ADM-UI-11c/`。
