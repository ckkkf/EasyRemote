# ADM-UI-12 验证记录（shadcn 默认主题 + 去掉自写组件）

> 时间：2026-11-24 08:20-08:45（Asia/Shanghai）

## 改动

| 文件 | 目的 |
|---|---|
| `src/styles/globals.css` | shadcn 官方默认主题（neutral HSL、`--radius:0.5rem`、dark）；`--muted-foreground` 46.1%→38%（AA 4.39→5.1，唯一偏离并在注释说明） |
| `tailwind.config.ts` | 颜色 → `hsl(var(--…))`；preflight 打开；圆角 `var(--radius)`；删除品牌令牌 |
| `src/styles.css` | 颜色改指 shadcn 变量；删除 `.btn`/`.link` 全部规则；新增统一 `focus-visible` |
| `components/ui/simple-select.tsx` | 删除；16 处调用点展开为 Select 组合 |
| `src/App.tsx` | 16 个下拉展开；33 处 `.btn/.link` → `buttonVariants(...)`；链接按钮 `min-h-6` |
| 测试 2 个 + E2E 2 个 | 断言与新形态对齐；基线常量 → ADM-UI-12 |

## 剩余自写组件

SimpleSelect 已删；`.btn`/`.link` CSS 已删；原生 select/checkbox/`.tbl` 均为 0。布局类（shell/side/nav/topbar/pager）仍在 styles.css。

## 断言 → 结果

| 断言 | 结果 |
|---|---|
| tsc | PASS |
| web 单测 | 58/58 |
| 全量 E2E | **PASS: 14 / FAIL: 0**（152s） |
| 响应式/可访问性 | **AUDIT OK** |
| 六屏判据 | 不变 |
| gate | **GATE: PASS** |

## 修掉的问题

1. 正则展开 Select 时把 `=>` 当标签结尾，16 块截断 → 逐块修复。
2. 删 `.btn` 后 a11y/stack 测试断言过期 → 改为守统一焦点规则与 shadcn Button 导入。
3. shadcn 默认 muted 前景对比度 4.39 → 调深至 38%（5.1）。
4. 链接按钮 18px → `min-h-6`。
5. 风控下拉 label 与 E2E 不一致 → 统一"风控类型"。

## 未完成

布局类未转 Tailwind；Tabs/Tooltip/DropdownMenu/Sheet 未引入；图表仍 SVG。
