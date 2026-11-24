# ADM-UI-03 A1 登录与身份迁移（TOTP）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-03（行 129-135）
> 需求：`docs/spec/pages.md` A1（行 197-199）、PRD §6.2 鉴权（行 553、670）、§6.7.1（行 700-708）
> 前置：ADM-UI-01a（React 18 + Tailwind + primitives）、ADM-UI-02a（路由）已完成
> 基线缺陷：ADM-UI-00 记录 `App.tsx` 登录页展示 `root / dev-pass` 与 `ER_ADMIN_TOTP_OFF` 提示（生产 UI 泄漏开发做法）

## 目标

把登录页迁到仓库内 shadcn primitives（Card / Input / Label / Button / Alert），保留真实 TOTP 登录、错误信封与 loading/error 行为，删除开发口令与关闭 MFA 的提示，并删掉与之重复的旧登录控件 CSS。

## 输入 / 输出

不改 API、不改契约、不改生成物，字段映射 **N/A**（登录仍用 `POST /admin/v1/login`，响应 `AdminSession`）。

## 必须通过的断言

- [ ] `TestLoginRendersShadcnForm`（`tests/login.test.tsx`，`renderToStaticMarkup`）：三个输入都有 id 且 label 通过 `htmlFor` 绑定；密码框 `type="password"`；验证码 `autocomplete="one-time-code"`；提交按钮 `type="submit"` 且带 `bg-primary` 变体类；不再存在 `class="login-form"`。
- [ ] `TestLoginHasNoDevCredentials`（同上）：渲染结果不含 `dev-pass`、`ER_ADMIN_TOTP_OFF`、`开发环境`；含「二次验证强制开启」。
- [ ] `TestLoginErrorShowsEnvelope`（同上）：`LoginError` 渲染 `role="alert"`，同时出现 message、`code`、`name` 与「复制 trace_id」。
- [ ] `TestLoginHasNoDevCredentialsInBundle`（`node scripts/check-web-admin-stack.mjs --build`）：`src/**` 与 `dist/assets/*.js` 都不得出现 `dev-pass` / `ER_ADMIN_TOTP_OFF`。
- [ ] `TestLoginE2E`（`python3 scripts/admin-ui-e2e-login.py`）：真实 stack + 真实 TOTP，8 条检查全 PASS —— 页面无开发口令、label 绑定、错误口令显示 `AUTH_INVALID_CREDENTIAL`+code+trace、停留登录页、正确凭据进入仪表盘、登录后仍无开发口令、`pageErrors` 为空。
- [ ] 既有回归全绿：`npx tsc --noEmit`、`npm test -- --run`、`npm run build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-03.md`、`docs/impl/evidence/ADM-UI-03/**`
- `web-admin/package.json`、`web-admin/package-lock.json`（`@radix-ui/react-label`）
- `web-admin/src/components/ui/label.tsx`、`alert.tsx`（新增 primitive）
- `web-admin/src/App.tsx`（**仅** 登录组件与新增的 `LoginError`、对应 import）
- `web-admin/src/styles.css`（**仅** 删除 `.login-form*` 旧控件样式；`.login-brand`/`.login-form-wrap` 布局保留）
- `web-admin/tests/login.test.tsx`
- `scripts/check-web-admin-stack.mjs`（新增 bundle 泄漏断言、primitives 清单加 label/alert）
- `scripts/admin-ui-e2e-login.py`

禁止：改其它页面、改 `docs/spec/**`（含 `error-codes.md` 文案）、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止引入 `react-hook-form`/`zod`（单表单不构成需求，留到多字段表单卡）；禁止 `git commit/revert/stash`。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-login.py
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (8 checks)`；gate `GATE: PASS`；截图与 JSON 证据落 `docs/impl/evidence/ADM-UI-03/`。

## 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/package.json web-admin/package-lock.json`，删除 `web-admin/src/components/ui/label.tsx`、`alert.tsx`、`tests/login.test.tsx`、`scripts/admin-ui-e2e-login.py`、`docs/impl/evidence/ADM-UI-03/`，再 `cd web-admin && npm ci`。
