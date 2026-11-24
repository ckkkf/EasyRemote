# ADM-UI-01a 验证证据

- 任务卡：`docs/impl/tasks/ADM-UI-01a.md`
- 执行时间：2026-11-23（Asia/Shanghai）
- 结论：六条新断言全绿；既有回归（csp / error-envelope / gate）全绿；`bash scripts/gate.sh` 末行 `GATE: PASS`。

## 1. 改动文件及目的

| 文件 | 类型 | 目的 |
|---|---|---|
| `web-admin/package.json` | 修改 | React/ReactDOM 降到 18.3.1，`@types/react`/`@types/react-dom` 对齐 18；新增 tailwindcss v3 / postcss / autoprefixer / clsx / tailwind-merge / class-variance-authority / @radix-ui/react-slot |
| `web-admin/package-lock.json` | 重新生成 | 锁文件可 `npm ci` 复现（react 18.3.1 / react-dom 18.3.1 / @types/react 18.3.31） |
| `web-admin/tailwind.config.ts` | 新增 | Tailwind v3 配置；`content` 覆盖 `index.html` + `src/**/*.{ts,tsx}`；`corePlugins.preflight=false`；颜色/圆角/字体引用 `design/generated/tailwind.tokens.cjs` |
| `web-admin/postcss.config.js` | 新增 | tailwindcss + autoprefixer |
| `web-admin/src/styles/globals.css` | 新增 | `@import` `design/generated/tokens.css`；语义变量全部映射 `--er-*`，不写死品牌色 |
| `web-admin/src/lib/utils.ts` | 新增 | `cn()` = clsx + tailwind-merge |
| `web-admin/src/components/ui/button.tsx` | 新增 | 仓库内 shadcn Button 源码（cva + Radix Slot），默认 `type="button"` |
| `web-admin/src/components/ui/input.tsx` | 新增 | 仓库内 shadcn Input 源码 |
| `web-admin/src/components/ui/card.tsx` | 新增 | 仓库内 shadcn Card 源码（Card/Header/Title/Description/Content/Footer） |
| `web-admin/src/components/ui/badge.tsx` | 新增 | 仓库内 shadcn Badge 源码（cva 变体） |
| `web-admin/src/main.tsx` | 修改 | 在 `./styles.css` 之前 import `./styles/globals.css`（1 行） |
| `web-admin/src/App.tsx` | 修改 | 顶部 import `Button`；登录表单提交按钮换成 `<Button type="submit">`，其余逻辑/文案/类名未动 |
| `web-admin/tests/ui-primitives.test.tsx` | 新增 | `TestButtonPrimitiveRenders` |
| `web-admin/tests/stack-conformance.test.ts` | 新增 | `TestWebAdminReact18`、`TestLoginUsesShadcnButton` |
| `scripts/check-web-admin-stack.mjs` | 新增 | `TestTailwindWiredToGeneratedTokens`、`TestShadcnPrimitivesAreSource`、`--build` 模式 `TestTailwindBuildEmitsUtility` |
| `docs/impl/evidence/ADM-UI-01a/**` | 新增 | 本证据目录 |

未改：`web-admin/src/styles.css`（既有规则零改动）、其它页面/组件、`docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`。
副作用：`web-admin/tsconfig.tsbuildinfo` 被 `npm run build`（`tsc -b`）重写，属生成缓存，不含源码改动。

## 2. 字段映射表

| 请求/响应字段 | spec 行号 |
|---|---|
| N/A（本卡不改任何 API、契约或生成物） | N/A |

## 3. 验收断言 → 测试名 → 结果

| 断言 | 测试名 | 位置 | 结果 |
|---|---|---|---|
| React 18 冻结值（package.json + 锁文件 + @types） | `TestWebAdminReact18` | `web-admin/tests/stack-conformance.test.ts` | PASS |
| Tailwind 接线到生成令牌、globals.css 无硬编码品牌色、postcss 含 tailwindcss+autoprefixer | `TestTailwindWiredToGeneratedTokens` | `scripts/check-web-admin-stack.mjs` | PASS |
| 构建产物含品牌色 + Tailwind 工具类 | `TestTailwindBuildEmitsUtility` | `scripts/check-web-admin-stack.mjs --build` | PASS |
| 四个 primitive 为仓库源码、含 `cn(`、无 fetch/axios、无 shadcn 运行时包 | `TestShadcnPrimitivesAreSource` | `scripts/check-web-admin-stack.mjs` | PASS |
| Button 渲染：outline 变体类、disabled、默认 type=button、asChild 走 Slot | `TestButtonPrimitiveRenders` | `web-admin/tests/ui-primitives.test.tsx` | PASS |
| 登录页从 `./components/ui/button` import 并使用 `<Button` | `TestLoginUsesShadcnButton` | `web-admin/tests/stack-conformance.test.ts` | PASS |
| 既有回归（csp / error-envelope / gate 三个旧测试） | 原测试名不变 | `web-admin/tests/{csp,error-envelope,gate}.test.ts` | PASS（10/10） |

## 4. 实际执行的命令与关键输出

```text
cd web-admin && npm ci
  added 149 packages in 5s            # npm-ci.log

cd web-admin && npx tsc --noEmit
  TSC_EXIT=0                          # tsc.log

cd web-admin && npm test -- --run
  ✓ tests/stack-conformance.test.ts (2 tests)
  ✓ tests/csp.test.ts (1 test)
  ✓ tests/gate.test.ts (3 tests)
  ✓ tests/error-envelope.test.ts (3 tests)
  ✓ tests/ui-primitives.test.tsx (1 test)
  Test Files  5 passed (5)
       Tests  10 passed (10)          # vitest.log

cd web-admin && npm run build
  dist/assets/index-CHVlNU0n.css   18.52 kB
  dist/assets/index-qnlMxPWh.js   203.54 kB
  BUILD_EXIT=0                        # build.log

cd .. && node scripts/check-web-admin-stack.mjs --build
  PASS TestTailwindWiredToGeneratedTokens · token 单一来源 + preflight 关闭
  PASS TestShadcnPrimitivesAreSource · 4 个源码 primitive
  PASS TestTailwindBuildEmitsUtility · index-CHVlNU0n.css · 品牌色 #1e4bdb + 工具类
  汇总: 3/3 通过                       # stack-check.log

bash scripts/gate.sh
  scripts   PASS
  contract  PASS
  go        PASS
  rust      PASS
  flutter   PASS
  web       PASS
  GATE: PASS                          # gate.log 末行
```

## 5. 构建产物与视觉证据

- `dist-css-snippet.txt`：品牌色 `--er-brand:#1e4bdb` 来自生成令牌；语义变量 `--background:var(--er-surface)`、`--ring:var(--er-brand)`；工具类 `.inline-flex`/`.h-11`/`.w-full`/`.mt-4`/`.bg-primary`/`.rounded-md{border-radius:8px}`；preflight 关闭证据：`border-width:0` 与 `::before` 在产物中各出现 0 次。
- `login-button-bbox.json`：同一 1360×860 视口下，登录提交按钮像素包围盒基线 `x=848,y=569,w=360,h=44`，改造后完全相同，delta 全 0 → 无布局位移。
- `login-dom-metrics.json`：按钮 `360×44`、`type="submit"`、`background-color: rgb(30, 75, 219)`（= #1E4BDB）、`border-radius: 8px`、无 pageerror；页面无滚动溢出（body 1360×860）。
- `a1-login-after.png`：改造后 A1 登录页截图，与 `docs/impl/evidence/ADM-UI-00/screens/a1-login.png` 对照，仅按钮圆角（pill → 8px）与字号（13/600 → 14/500）不同，属卡内允许范围。

## 6. 未完成项、限制与风险

- 未完成项：无。六条断言与既有回归全绿，gate PASS。
- 本卡未迁移任何业务页面（仅登录提交按钮接线）；页面迁移按 ADM-UI-02/03 另卡执行。
- `web-admin/src/styles.css` 中仍有与 shadcn primitives 重复的 button/input 基础样式（PRD §6.7 收口在后续卡），本卡按约束未清理。
- Tailwind 预检关闭意味着 UA 默认样式仍在：Button 基类显式加了 `border border-transparent` 归一 UA 边框，避免与 variant 的 border 叠两层。
- 主题策略：`globals.css` 已给出 `[data-theme='dark']` 语义映射，但当前未提供切换入口（无 data-theme 设置方），属后续卡范围。
- 无 mock/跳过测试；未改动任何测试基线或 spec。

## 7. 回滚

```bash
git checkout -- web-admin/package.json web-admin/package-lock.json web-admin/src/main.tsx web-admin/src/App.tsx
rm -rf web-admin/tailwind.config.ts web-admin/postcss.config.js \
       web-admin/src/styles web-admin/src/lib web-admin/src/components \
       web-admin/tests/ui-primitives.test.tsx web-admin/tests/stack-conformance.test.ts \
       scripts/check-web-admin-stack.mjs
cd web-admin && npm ci
```
（`docs/impl/evidence/ADM-UI-01a/` 可整目录删除；`web-admin/dist` 与 `tsconfig.tsbuildinfo` 为构建产物。）

## 8. 证据文件清单

| 文件 | 内容 |
|---|---|
| `npm-ci.log` | `npm ci` 输出 |
| `tsc.log` | `npx tsc --noEmit` 输出 |
| `vitest.log` | `npm test -- --run` 完整输出 |
| `build.log` | `npm run build` 输出 |
| `stack-check.log` | `node scripts/check-web-admin-stack.mjs --build` 输出 |
| `gate.log` | `bash scripts/gate.sh` 完整输出（末行 GATE: PASS） |
| `dist-css-snippet.txt` | dist CSS 关键片段 |
| `login-button-bbox.json` | 改造前后按钮像素包围盒 |
| `login-dom-metrics.json` | 登录页 DOM/样式实测值 |
| `a1-login-after.png` | 改造后 A1 登录页截图 |
| `git-diff-stat.txt`、`git-status.txt` | 工作区变更范围 |

## 9. 主会话独立复核（2026-11-23 10:29）

本卡的实现由 Ralph 单轮 worker 完成；主会话的 `ralph` 调用在 600s 工具上限处被中断，因此以下每一项都由主会话**重新执行**，不采信 worker 自述：

| 复核项 | 主会话实际命令 | 结果 |
|---|---|---|
| 依赖可复现 | `cd web-admin && npm ci` | 成功 |
| 类型 | `npx tsc --noEmit` | `TSC_EXIT=0` |
| 测试 | `npm test -- --run` | 5 files / 10 tests 全绿（含旧 csp / error-envelope / gate） |
| 构建 | `npm run build` | `dist/assets/index-CHVlNU0n.css 18.52 kB`、`index-qnlMxPWh.js 203.54 kB` |
| 技术栈断言 | `node scripts/check-web-admin-stack.mjs --build` | `汇总: 3/3 通过` |
| 闸门 | `bash scripts/gate.sh` | 20/20 PASS，末行 `GATE: PASS` |
| 几何（独立测量） | 静态服务 dist + playwright 量登录按钮 | `x=848.28, y=569.31, w=360, h=44`，与改造前基线 `848/569/360/44` 一致，delta≈0；`background-color: rgb(30, 75, 219)`（=#1E4BDB，来自生成令牌）、`border-radius: 8px`、`type=submit`、`pageErrors: []` |
| 作用域 | `git status --porcelain` | 只出现卡内白名单文件 + `tsconfig.tsbuildinfo`（`tsc -b` 生成缓存）；`src/styles.css` 零改动 |

结论：worker 的自述与主会话独立复跑一致，本卡可标记完成。

