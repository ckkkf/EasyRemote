# ADM-UI-11c 验证记录（全站 shadcn 控件）

> 时间：2026-11-23 20:21-21:25（Asia/Shanghai）

## 改动

| 文件 | 目的 |
|---|---|
| `web-admin/src/components/ui/select.tsx` | shadcn Select（Radix，官方源码） |
| `web-admin/src/components/ui/simple-select.tsx` | 页面级下拉薄封装（16 处调用） |
| `web-admin/src/components/ui/switch.tsx` | shadcn Switch（h-6 满足 24px 目标） |
| `web-admin/src/components/ui/separator.tsx` | shadcn Separator（备用） |
| `web-admin/src/App.tsx` | 16 个 select → SimpleSelect；3 个开关 → Switch；14 个输入 → Input；空值哨兵 |
| `web-admin/src/styles.css` | 顶栏换行/收缩、卡片最小宽度 |
| `scripts/admin_e2e.py` | `pick()` 交互（等可用 → 点触发器 → 点选项） |
| `scripts/admin-ui-e2e-*.py` | 31 处 select_option → pick；行点击改点单元格；配置作用域等真实 GET |
| `web-admin/package.json` | +4 个 shadcn 依赖 |

## 断言 → 结果

| 断言 | 结果 |
|---|---|
| tsc | PASS |
| web 单测 | 57/57 |
| 全量 E2E | `PASS: 14 / FAIL: 0`（150s） |
| 响应式/可访问性 | `AUDIT OK`（28 张截图） |
| 六屏判据 | 不变（tables 2 / filters 4 / config 4-4-7） |
| 视觉基线 | 新基线 `ADM-UI-11c/screens/manifest.json` |
| 闸门 | `GATE: PASS` |

## 过程中修掉的问题

1. Radix Select 空字符串 value 直接报错 → 引入 `SENTINEL`。
2. shadcn Switch 默认 20px 触发 `small-target` → 改 24px。
3. 顶栏按钮 nowrap 把 768/1024 视口顶出 → `flex-wrap` + `min-width:0`。
4. `<tr>` 点击被 `<table>` 拦截（Playwright 命中测试）→ 点首个 `<td>`。
5. 切换作用域后立刻读输入会拿到旧值 → 用 `expect_response` 等真实 GET 返回。
6. `pick()` 在控件禁用时等 30s 超时 → 先等 `!el.disabled` 再点。

## 未做 / 风险

- 31 处 `.btn` 标记仍是原生 button（视觉一致，语义未迁移）。
- 未引入 Tabs/Tooltip/DropdownMenu/Sheet/Sonner/Command；图表仍 SVG。
- 新增 4 个依赖，bundle 410.9 kB（gzip 129.4 kB）。

## 回滚

`git checkout --` App.tsx/styles.css/components/scripts；删除新增 primitive、`admin_e2e.py`、`evidence/ADM-UI-11c/`。
