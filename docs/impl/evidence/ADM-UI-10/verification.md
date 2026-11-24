# ADM-UI-10a 验证记录

> 卡：`docs/impl/tasks/ADM-UI-10a.md`
> 时间：2026-11-23 11:46-11:56（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/styles.css` | `--ink-3` 改为 #5c616b（AA 对比度）、`.link` 最小 24px 交互目标、`:focus-visible` 焦点环、≤860px `align-content:start` 修空白 |
| `web-admin/src/styles/globals.css` | `prefers-reduced-motion` 媒体查询 |
| `web-admin/tests/a11y.test.ts` | 3 条静态契约断言（动效/焦点/对比度现算） |
| `scripts/admin-ui-responsive-a11y.py` | 4 档宽度 × 7 页审计（溢出/表格遮挡/目标尺寸/键盘/对比度/动效） |

## 2. 字段映射

N/A（纯样式与审计）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| reduced-motion 生效 | `TestReducedMotionHonoured` | PASS |
| focus-visible + 24px 目标 | `TestFocusVisibleStyles` | PASS |
| 次要文字对比度 ≥ 4.5（现算） | `TestMutedTextContrastMeetsAA` | PASS |
| 28 张截图、0 findings | `TestResponsiveA11yAudit` | PASS（`AUDIT OK`） |
| 六屏 DOM 判据 | `TestVisualBaselineUnchanged` | PASS |
| 回归/技术栈/闸门 | `npm test`（50）/ `stack-check` 4/4 / `gate.sh` | 全绿 |

## 4. 审计发现与修复（首轮 22 findings → 0）

| 发现 | 数量 | 修复 |
|---|---|---|
| `small-target`：「退出登录」高度 < 24px | 21（每页每宽度一次） | `.link` min-height 24px |
| `contrast`：3.87 < 4.5 | 1 | `--ink-3` → #5c616b |
| （人工看图）≤860px 导航下方大片空白 | — | `.shell` `align-content:start` |

## 5. 关键输出

```text
Tests  50 passed (50)
screenshots: 28
AUDIT OK
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

截图目录：`docs/impl/evidence/ADM-UI-10/shots/`（`1360x860-*`、`1280x800-*`、`1024x768-*`、`768x1024-*` 各 7 页）。

## 6. 未完成项、限制与风险

- **E2E 覆盖缺口**：真实登录、列表筛选、详情、中断（成功/取消）已覆盖；**登出、分页翻页、中断 partial（传播失败）** 尚未有端到端用例——partial 需要可控的传播失败注入（目前只有单进程 stack），建议单独开 ADM-UI-10b。
- 审计脚本只做几何/样式断言，不做像素 diff；像素基线仍是 `admin-ui-baseline.py` 的六屏 DOM 判据。
- 对比度只抽样正文与次要文字，没有逐元素全量扫描（可用 axe-core 做更全的扫描，属后续增强）。
- 1024 宽度下表格若继续变宽会依赖浏览器默认收缩；当前无遮挡，但列多起来需要显式横向滚动容器。

## 7. 回滚

`git checkout -- web-admin/src/styles.css web-admin/src/styles/globals.css`；删除 `web-admin/tests/a11y.test.ts`、`scripts/admin-ui-responsive-a11y.py`、`docs/impl/evidence/ADM-UI-10/`。
