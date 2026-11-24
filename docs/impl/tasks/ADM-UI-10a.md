# ADM-UI-10a 视觉/可访问性/响应式回归

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-10（行 186-192）
> 需求：PRD §6.7（行 700-774）、`docs/spec/pages.md` 三、页面覆盖检查（行 274-286）
> 前置：ADM-UI-00（六屏基线）、01a-09a 各页迁移

## 目标

建立可执行的响应式与可访问性审计（4 档宽度 × 7 个页面），修掉审计发现的真实问题：对比度不足、交互目标过小、窄屏空白拉伸；补齐 reduced-motion 与焦点可见样式，并把这三类约束固化成测试。

## 输入 / 输出

不改 API、不改契约，字段映射 **N/A**。

## 审计口径（可执行，不靠"看起来还行"）

| 检查 | 判据 |
|---|---|
| 页面横向溢出 | `documentElement.scrollWidth > innerWidth + 1` 即失败 |
| 表格遮挡 | 表格 `scrollWidth > 容器 clientWidth` 且容器不可横向滚动即失败 |
| 交互目标 | 任何可见 button/导航链接高度 < 24px 即失败 |
| 键盘焦点 | Tab 后 `document.activeElement` 必须可见且有 outline 或 box-shadow |
| 对比度 | 正文/次要文字与背景对比度 ≥ 4.5（WCAG AA） |
| 减少动效 | `prefers-reduced-motion: reduce` 下 transition/animation ≤ 10ms |

## 本次修复

| 问题 | 修复 |
|---|---|
| `--ink-3` (#777c87) 在 `--surface-2` 上对比度 3.87 | 改为 #5c616b（≥4.5），并在测试里锁死 |
| 「退出登录」等文本链接高度 20px | `.link` 加 `min-height:24px; display:inline-flex; align-items:center` |
| 缺 `:focus-visible` 焦点环 | 为 `.link/.btn/.nav/input/select/textarea` 加 2px brand outline |
| 无 reduced-motion 策略 | `globals.css` 加 `prefers-reduced-motion` 媒体查询 |
| ≤860px 单列后导航下方大片空白 | `.shell` 加 `align-content:start`，避免 grid 行被 `min-height:100vh` 拉伸 |

## 必须通过的断言

- [ ] `TestReducedMotionHonoured`：globals.css 含 `prefers-reduced-motion` 与 `transition-duration: 0.01ms`。
- [ ] `TestFocusVisibleStyles`：styles.css 含 `:focus-visible` + 2px brand outline + `.link` min-height 24px。
- [ ] `TestMutedTextContrastMeetsAA`：`--ink-3` 与 `--surface-2` 的对比度 ≥ 4.5（测试里现算）。
- [ ] `TestResponsiveA11yAudit`（`python3 scripts/admin-ui-responsive-a11y.py`）：4 档宽度 × 7 页共 28 张截图，0 findings，输出 `AUDIT OK`。
- [ ] `TestVisualBaselineUnchanged`：六屏 DOM 判据不变（颜色变化不影响结构判据）。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-10a.md`、`docs/impl/evidence/ADM-UI-10/**`
- `web-admin/src/styles.css`（`--ink-3`、`.link`、`:focus-visible`、≤860 媒体查询）
- `web-admin/src/styles/globals.css`（reduced-motion）
- `web-admin/tests/a11y.test.ts`
- `scripts/admin-ui-responsive-a11y.py`

禁止：改页面结构/文案/请求逻辑、改 `docs/spec/**`、`server/**`、`prototype/**`；禁止为通过审计放宽阈值。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-responsive-a11y.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-10/screens --compare-to docs/impl/evidence/ADM-UI-08/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；`AUDIT OK`（28 张截图）；六屏判据不变；gate `GATE: PASS`。

## 回滚

`git checkout -- web-admin/src/styles.css web-admin/src/styles/globals.css`；删除 `web-admin/tests/a11y.test.ts`、`scripts/admin-ui-responsive-a11y.py`、`docs/impl/evidence/ADM-UI-10/`。
