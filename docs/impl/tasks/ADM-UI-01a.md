# ADM-UI-01a 技术栈收口：React 18 + Tailwind + 设计令牌 + 仓库内 shadcn primitives

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-01（行 113-119）、§5（行 65-100）
> 需求：PRD §6.4（行 655-673）、§6.7.1（行 700-708）、§6.7.3（行 726-742）
> 前置：ADM-UI-00 已完成（`docs/impl/evidence/ADM-UI-00/verification.md`）
> 拆分说明：ADM-UI-01 同时包含"工具链"与"逐页迁移"。本卡只做工具链与首批 primitives，**不迁移任何业务页面**（登录页仅把提交按钮换成 `Button` 作为接线证明），页面迁移按 ADM-UI-02/03 另卡执行。

## 目标

把 web-admin 的技术栈对齐 PRD 冻结值：React 18 + TypeScript + Vite + Tailwind + 仓库内维护的 shadcn 组件源码 + 生成的设计令牌，且不破坏现有页面、测试与闸门。

## 输入 / 输出

本卡不改任何 API、不改契约、不改生成物，字段映射 **N/A**（无请求/响应字段变化）。

产物：

| 产物 | 路径 | 要求 |
|---|---|---|
| React 18 | `web-admin/package.json`、`web-admin/package-lock.json` | `react`/`react-dom` = `^18.3.1`；`@types/react`/`@types/react-dom` = `^18.3.x`；锁文件可 `npm ci` |
| Tailwind 工具链 | `web-admin/tailwind.config.ts`、`web-admin/postcss.config.js` | Tailwind v3；`content` 覆盖 `index.html` 与 `src/**/*.{ts,tsx}`；**`corePlugins.preflight: false`**（现有 `styles.css` 仍生效，避免一次性重排视觉） |
| 令牌单一来源 | `web-admin/tailwind.config.ts` ← `design/generated/tailwind.tokens.cjs`；`web-admin/src/styles/globals.css` ← `design/generated/tokens.css` | 禁止在 `globals.css` 手写 `#1E4BDB` 之类色值；语义变量必须引用生成令牌 |
| shadcn primitives | `web-admin/src/components/ui/{button,input,card,badge}.tsx` | 源码复制进仓库；用 `class-variance-authority` + `cn()`；**禁止**引入 `shadcn`/`@shadcn/ui` 运行时包 |
| `cn` 工具 | `web-admin/src/lib/utils.ts` | `clsx` + `tailwind-merge` |
| 入口接线 | `web-admin/src/main.tsx` | 在 `./styles.css` 之前 import `./styles/globals.css` |
| 接线证明 | `web-admin/src/App.tsx` | **仅**把登录表单提交按钮换成 `<Button>`；其余逻辑、文案、类名保持不变 |
| 断言脚本 | `scripts/check-web-admin-stack.mjs` | 见下 |
| 测试 | `web-admin/tests/ui-primitives.test.tsx`、`web-admin/tests/stack-conformance.test.ts` | 见下 |

## 必须通过的断言

- [x] `TestWebAdminReact18`（`tests/stack-conformance.test.ts`）：`package.json` 的 react/react-dom 主版本为 18；`package-lock.json` 中 `node_modules/react` 的 `version` 以 `18.` 开头；`@types/react` 主版本 18。
- [x] `TestTailwindWiredToGeneratedTokens`（`scripts/check-web-admin-stack.mjs`）：`tailwind.config.ts` 引用了 `design/generated/tailwind.tokens.cjs`；`src/styles/globals.css` 引用 `design/generated/tokens.css`；`globals.css` 中不出现硬编码 `#1E4BDB`/`#1e4bdb`；`postcss.config.js` 含 tailwindcss + autoprefixer。
- [x] `TestTailwindBuildEmitsUtility`（同上，`--build` 模式）：构建后 `dist/assets/*.css` 必须同时包含生成令牌里的品牌色（大小写不敏感匹配 `1e4bdb`）与至少一条 Tailwind 工具类产物（如 `.inline-flex`）。
- [x] `TestShadcnPrimitivesAreSource`（同上）：四个 primitive 文件存在、含 `cn(` 调用、`components/ui` 下无 `fetch(`/`axios`；`package.json` 依赖里没有名为 `shadcn` 或 `@shadcn/ui` 的包。
- [x] `TestButtonPrimitiveRenders`（`tests/ui-primitives.test.tsx`，`renderToStaticMarkup`，无需 jsdom）：`variant="outline"` 输出含 outline 变体类；`disabled` 时输出 `disabled`；默认 `type="button"`；`asChild` 走 Radix Slot。
- [x] `TestLoginUsesShadcnButton`（`tests/stack-conformance.test.ts`）：`src/App.tsx` 从 `./components/ui/button` import 了 `Button`，且登录表单提交按钮使用 `<Button`。
- [x] 既有回归全绿：`npx tsc --noEmit`、`npm test -- --run`（含 csp / error-envelope / gate 三个旧测试）、`npm run build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-01a.md`（本卡）
- `docs/impl/evidence/ADM-UI-01a/**`（命令输出、构建产物片段、未完成项）
- `web-admin/package.json`、`web-admin/package-lock.json`
- `web-admin/tailwind.config.ts`、`web-admin/postcss.config.js`
- `web-admin/src/styles/globals.css`
- `web-admin/src/lib/utils.ts`
- `web-admin/src/components/ui/button.tsx`、`input.tsx`、`card.tsx`、`badge.tsx`
- `web-admin/src/main.tsx`
- `web-admin/src/App.tsx`（**仅**登录提交按钮一处 + 顶部 import）
- `web-admin/tests/ui-primitives.test.tsx`、`web-admin/tests/stack-conformance.test.ts`
- `scripts/check-web-admin-stack.mjs`

禁止：改 `web-admin/src/styles.css` 的既有规则、改其它页面组件、改 `docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止 `git commit/revert/stash`；禁止用 `!important` 或 Tailwind preflight 重排现有视觉。

## 验收命令

```bash
cd web-admin && npm ci && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
bash scripts/gate.sh
```

## 完成标准

七条断言全绿；`npm ci` 可复现；旧测试与 gate 不回退；登录页视觉不出现明显位移（`docs/impl/evidence/ADM-UI-00/screens/a1-login.png` 对照，允许按钮圆角/字号差异，不允许布局位移）；证据落 `docs/impl/evidence/ADM-UI-01a/`。

## 回滚

`git checkout -- web-admin/package.json web-admin/package-lock.json web-admin/src/main.tsx web-admin/src/App.tsx`，并删除本卡新增的 `tailwind.config.ts`、`postcss.config.js`、`src/styles/`、`src/lib/`、`src/components/`、两个测试文件与 `scripts/check-web-admin-stack.mjs`。