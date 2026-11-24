# ADM-UI-12 shadcn 默认主题 + 去掉自写组件

用户要求：用 shadcn 的组件、配色用默认的。

## 已完成
1. 主题换 shadcn 默认（neutral HSL、0.5rem、preflight 打开）；
2. 删除 SimpleSelect（16 处展开为 Select 组合）；
3. 删除 `.btn`/`.link` 桥接 CSS（33 处改 `buttonVariants`）；
4. 可访问性门槛不放宽：AA/AAA 仍由测试守，AUDIT OK。

## 验收
web 单测 58/58；E2E PASS 14/0；AUDIT OK；GATE PASS。证据见 `evidence/ADM-UI-12/verification.md`。

## 未做
布局类仍是手写 CSS；Tabs/Tooltip/DropdownMenu/Sheet 未引入；图表仍 SVG。
