# ADM-UI-10b 生命周期 E2E：服务端分页 / 服务端登出 / 中断结果语义

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-10（行 190：Playwright 覆盖真实登录/列表过滤/分页/详情/中断 partial/登出）
> 前置：ADM-UI-10a（响应式/可访问性）、ADM-UI-07a（中断语义）

## 目标

补齐 ADM-UI-10 明确要求但尚未覆盖的三段：**服务端分页翻页**、**服务端登出（令牌真的失效）**、**中断 partial 结果语义**。其中 partial 无法在本地单进程 stack 注入传播失败，因此把结果判定抽成纯函数用单测覆盖，E2E 覆盖成功/取消路径（已有）。

## 输入 / 输出

不改 API、不改契约，字段映射 **N/A**。

## 必须通过的断言

- [ ] `TestKillResultOkRequiresClosedPropagatedAudited`：三者全真才是成功，文案含会话 ID。
- [ ] `TestKillResultPartialOnPropagationFailure`：传播失败 → `partial`，文案不得出现"均已确认"。
- [ ] `TestKillResultPartialOnAuditFailure`：审计失败 → `partial`。
- [ ] `TestKillResultErrorWhenNotClosed`：未关闭 → `error`。
- [ ] `TestLifecycleE2E`（`python3 scripts/admin-ui-e2e-lifecycle.py`）：11 条全 PASS —— 第一页 10 行、分页文案 12 条/2 页、下一页只剩 2 行且与第一页不相交、返回第一页恢复 10 行、登出前令牌有效、登出后落登录页并清空 sessionStorage、**旧令牌再请求返回 401 ADMIN_SESSION_EXPIRED**、未登录深链回登录页、无 pageerror。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-10b.md`、`docs/impl/evidence/ADM-UI-10b/**`
- `web-admin/src/features/sessions/kill-result.ts`（新增）
- `web-admin/src/App.tsx`（Sessions.kill 改用 `describeKillResult`）
- `web-admin/tests/kill-result.test.ts`
- `scripts/admin-ui-e2e-lifecycle.py`

禁止：改 `docs/spec/**`、`server/**`、`prototype/**`；禁止把 partial 结果说成成功；禁止用 `ER_RATELIMIT_OFF` 以外的服务端改动绕过限流（该开关生产配置会启动失败）。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-lifecycle.py
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；E2E `E2E OK (11 checks)`；gate `GATE: PASS`。

## 回滚

删除 `kill-result.ts`、`tests/kill-result.test.ts`、`scripts/admin-ui-e2e-lifecycle.py`、`docs/impl/evidence/ADM-UI-10b/`；`git checkout -- web-admin/src/App.tsx`。
