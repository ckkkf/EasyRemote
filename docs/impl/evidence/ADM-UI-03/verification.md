# ADM-UI-03 验证记录

> 卡：`docs/impl/tasks/ADM-UI-03.md`
> 时间：2026-11-23 10:40-10:50（Asia/Shanghai）
> 基线 commit：`267a635`

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/components/ui/label.tsx` | 新增 shadcn Label 源码（Radix Label，`htmlFor` 绑定） |
| `web-admin/src/components/ui/alert.tsx` | 新增 shadcn Alert 源码（`destructive` 变体用于错误信封） |
| `web-admin/src/App.tsx` | 登录页改用 `UiCard/Input/Label/Button/Alert`；新增 `LoginError`（message + code/name + 复制 trace_id）；删除开发口令提示；导出 `Login`/`LoginError` 供测试 |
| `web-admin/src/styles.css` | 删除 `.login-form*` 旧控件样式（避免与 primitives 双写同一属性）；`.login-brand`/`.login-form-wrap` 布局保留 |
| `web-admin/package.json`、`package-lock.json` | `@radix-ui/react-label ^2.1.8` |
| `web-admin/tests/login.test.tsx` | 3 条登录断言（SSR 渲染，无需 jsdom） |
| `scripts/check-web-admin-stack.mjs` | 新增 `TestLoginHasNoDevCredentialsInBundle`（src + dist 双查）；primitives 清单加 label/alert |
| `scripts/admin-ui-e2e-login.py` | 真实 TOTP 登录 E2E（8 条检查 + 截图） |

未改：其它页面、`docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`。

## 2. 字段映射

N/A（`POST /admin/v1/login` 与 `AdminSession` 未变，契约与生成物零改动）。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三个控件 id/label 绑定、密码 type、验证码 autocomplete、submit 按钮变体类、旧 `.login-form` 已消失 | `TestLoginRendersShadcnForm` | PASS |
| 渲染结果无开发口令/MFA 绕过提示 | `TestLoginHasNoDevCredentials` | PASS |
| 错误信封 message + code + name + 复制 trace_id 同时可见 | `TestLoginErrorShowsEnvelope` | PASS |
| 源码与构建产物都无 `dev-pass`/`ER_ADMIN_TOTP_OFF` | `TestLoginHasNoDevCredentialsInBundle` | PASS |
| 真实 TOTP 登录：错误口令 → 信封并停留；正确凭据 → 仪表盘 | `TestLoginE2E` | PASS（8/8） |
| 既有回归 / 类型 / 构建 / 闸门 | `npm test -- --run` / `tsc` / `build` / `gate.sh` | 7 files / 18 tests PASS；`GATE: PASS` |

## 4. 命令与关键输出

见 `commands.txt`。关键行：

```text
Tests  18 passed (18)
PASS TestLoginHasNoDevCredentialsInBundle · 无开发口令泄漏（src + dist/index-6kNViu46.js）
汇总: 4/4 通过
PASS wrong password shows envelope -> 手机号或验证码不正确 | 100001 AUTH_INVALID_CREDENTIAL | 复制 trace_id
PASS valid credentials reach dashboard -> http://127.0.0.1:5395/dashboard
E2E OK (8 checks)
GATE: PASS
```

## 5. 视觉证据

- `login.png`（1360×860）：shadcn Card 包裹表单，输入框 36px 高、8px 圆角、focus ring 来自 `--ring`；label 12→14px 且与控件绑定；底部说明改为「仅限已授权的运营与客服账号 · 二次验证强制开启」。
- `login-error.png`（1360×860）：`Alert variant=destructive` 展示 `100001 AUTH_INVALID_CREDENTIAL` 与「复制 trace_id」。
- `dashboard-after-login.png`：登录成功后进入 `/dashboard`（路由卡已接管跳转）。

## 6. 未完成项、限制与风险

- **错误文案不匹配场景（spec 侧问题，本卡未改）**：`AUTH_INVALID_CREDENTIAL` 在 `docs/spec/error-codes.md` 的文案是「手机号或验证码不正确」（C 端短信登录口径），后台账号密码登录复用了同一码，界面显示给运营的提示词不准确。修文案要改 spec 并由负责人批准，登记为待办（建议随 ADM-UI-08 或独立 error-copy 卡处理）。
- 本卡未引入 `react-hook-form + zod`：登录只有 3 个字段，且校验以服务端为准；等出现多字段表单（配置/用户操作）再引入，避免为套库而套库。
- 登录后的身份/权限仍来自登录响应（ER-810 行为保留）；服务端仍独立校验，前端隐藏不等于授权。
- 登出仍走 `POST /admin/v1/logout` 并清 sessionStorage（ER-811 的服务端撤销语义未改）。
- 响应式：`@media` 里 `.login-brand{display:none}` 仍生效，窄屏只显示表单卡片。

## 7. 回滚

`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/package.json web-admin/package-lock.json`，删除 `web-admin/src/components/ui/label.tsx`、`alert.tsx`、`web-admin/tests/login.test.tsx`、`scripts/admin-ui-e2e-login.py`、`docs/impl/evidence/ADM-UI-03/`，再 `cd web-admin && npm ci`。
