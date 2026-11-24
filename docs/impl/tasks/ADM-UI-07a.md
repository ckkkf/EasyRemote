# ADM-UI-07a 会话列表/详情/中断确认（A7 / A8 / A9）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-07（行 162-168）
> 需求：`docs/spec/pages.md` A7（行 234-238）、A8（行 240-242）、A9（行 244-246）、PRD §6.3.4（行 603-607）
> 前置：ADM-UI-05a/06a（DataTable + 详情路由范式）、ADM-UI-02c（Radix Dialog）

## 目标

1. **修复状态枚举错位**（ADM-UI-00 记录的缺陷）：筛选与禁用条件用 `ended`，而状态机终态是 `closed`；
2. **修复状态筛选静默失效**：前端发 `status=closed`，服务端读的是 `state`，筛选从未生效；
3. 会话列表改 DataTable + 中文状态标签 + 行点击进 `/sessions/:id`；
4. A9 中断对话框：原因类别下拉 + 补充说明 + 审计原因预览 + 传播/审计结果语义（ER-812 不变）；
5. A8 会话详情独立路由：会话字段真实展示，每秒质量曲线/链路切换/错误时间线明确「接口未提供」。

## 输入 / 输出

不改 API 契约。字段映射：

| 展示项 | 来源 |
|---|---|
| 会话 ID / 主控 / 被控 / 状态 / 开始/结束时间 / 原因 | `SessionRow`（`contract.yaml`） |
| 时长 | 由 `started_at`/`ended_at` 计算（未结束用当前时间） |
| 状态枚举 | `server/internal/session/session.go:19-22`（requested/authorized/active/closed） |
| 每秒质量曲线 / 链路切换 / 错误码时间线 | 无 telemetry → 「接口未提供（ADM-API-02）」 |
| 中断原因类别 | UI 常量（PRD A9 要求下拉 + 自定义），拼接后作为 `reason` 送审计 |

## 必须通过的断言

- [ ] `TestSessionStatesMatchServerMachine`：枚举恰为四个状态，且不含 `ended`。
- [ ] `TestTerminalStateIsClosed`：只有 `closed` 是终态（决定中断按钮禁用）。
- [ ] `TestStateLabelsAndTones`：中文标签与色调映射。
- [ ] `TestKillReasonCategories` / `TestComposeKillReason` / `TestKillReasonValidation`：类别、拼接、校验（未选类别不可提交）。
- [ ] `TestSessionsE2E`（`python3 scripts/admin-ui-e2e-sessions.py`）：18 条全 PASS —— 列、筛选含四态且无 ended、**closed 筛选真的筛到 1 行**、closed 行中断禁用、对话框需选类别、审计原因预览、取消不写审计、确认后会话真变 closed 且写审计、详情路由/深链/返回/未知会话、无 pageerror。
- [ ] `TestVisualBaselineUnchanged`：与 ADM-UI-06 基线相比只有 a7 的**有意**变化（状态列由原始英文改为中文标签），其余 5 屏一致。
- [ ] 既有回归：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-07a.md`、`docs/impl/evidence/ADM-UI-07/**`
- `web-admin/src/features/sessions/{session-states.ts,kill-reasons.ts,session-detail-page.tsx}`（新增）
- `web-admin/src/App.tsx`（Sessions 改 DataTable + 新中断对话框 + `:id` 路由）
- `web-admin/src/api.ts`（`state` 查询参数：类型 + 序列化；这是筛选失效的根因修复）
- `web-admin/tests/session-states.test.ts`、`tests/kill-reasons.test.ts`
- `scripts/admin-ui-e2e-sessions.py`

禁止：改 `docs/spec/**`、`server/**`、`prototype/**`；禁止把传播/审计结果合并成单一"成功"提示；禁止画假曲线。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-sessions.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-07/screens --compare-to docs/impl/evidence/ADM-UI-06/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

六条断言全绿；E2E `E2E OK (18 checks)`；视觉差异只有 a7 状态标签；gate `GATE: PASS`。

## 回滚

删除 `features/sessions/`、`tests/session-states.test.ts`、`tests/kill-reasons.test.ts`、`scripts/admin-ui-e2e-sessions.py`、`docs/impl/evidence/ADM-UI-07/`；`git checkout -- web-admin/src/App.tsx web-admin/src/api.ts`。
