# ADM-UI-02c 对话框迁移：Radix Dialog / ConfirmDialog / DetailDialog

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-02（行 125：ConfirmDialog）、ADM-UI-07（行 166：A9 用 shadcn Dialog/AlertDialog）
> 需求：PRD §6.1 第 3 条（行 551「敏感操作二次确认」）、§6.7.1（行 704-708）
> 前置：ADM-UI-01a（primitives）、02a（路由）、02b（三态）

## 目标

把自绘的 `.modal-mask/.modal/.modal-actions` 换成仓库内 Radix Dialog 源码，并抽出 `ConfirmDialog`（封禁/中断）与 `DetailDialog`（设备/用户详情）：焦点陷阱、Esc 关闭、`aria-labelledby`、外部内容 `aria-hidden` 全部由 Radix 负责；确认按钮的可用性仍由业务规则（原因字数）决定。

## 输入 / 输出

不改 API、不改契约、不改生成物，字段映射 **N/A**。

## 必须通过的断言

- [ ] `TestDialogsUseRadixDialog`（`tests/dialog-usage.test.ts`）：`App.tsx` 不再有本地 `Modal`、`modal-mask`、`modal-actions`，改为引用 `confirm-dialog`/`detail-dialog`；`components/ui/dialog.tsx` 基于 `@radix-ui/react-dialog`；`styles.css` 不再有 `.modal-mask`/`.modal-actions`。
- [ ] `TestConfirmDialogGatesDangerAction`（同上）：危险操作走 `destructive`、`disabled={busy || confirmDisabled}`、关闭统一走 `onOpenChange`。
- [ ] `TestDialogE2E`（`python3 scripts/admin-ui-e2e-dialogs.py`）：16 条检查全 PASS —— role=dialog、aria-labelledby、外部 `aria-hidden`、焦点在框内、原因不足时确认禁用、Esc 关闭且**不产生副作用**、封禁真实生效并写审计、取消中断**不写审计**、确认中断写审计、设备详情 6 组 kv、用户详情异步表格、无 pageerror。
- [ ] `TestVisualBaselineUnchanged`（`admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-02c/screens --compare-to ADM-UI-00 基线`）：六屏 DOM 判据一致（a6 的判据改从 `[role=dialog]` 读取）。
- [ ] 既有回归全绿：`tsc`、`npm test -- --run`、`npm run build`、`check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-02c.md`、`docs/impl/evidence/ADM-UI-02c/**`
- `web-admin/package.json`、`web-admin/package-lock.json`（`@radix-ui/react-dialog`）
- `web-admin/src/components/ui/dialog.tsx`、`textarea.tsx`（新增 primitive）
- `web-admin/src/components/business/confirm-dialog.tsx`、`detail-dialog.tsx`（新增）
- `web-admin/src/App.tsx`（仅 4 处对话框调用点 + import + 删除本地 Modal）
- `web-admin/src/styles.css`（删除 `.modal*` 规则；`.modal .kv` 改为 `.kv`）
- `web-admin/tests/dialog-usage.test.ts`
- `scripts/admin-ui-baseline.py`（a6 选择器改 `[role=dialog]`）
- `scripts/admin-ui-e2e-dialogs.py`

禁止：改页面列/筛选/请求逻辑、改 `docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止放宽基线判据；禁止 `git commit/revert/stash`。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-e2e-dialogs.py
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-02c/screens --compare-to docs/impl/evidence/ADM-UI-00/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

四条断言全绿；E2E `E2E OK (16 checks)`；基线比对 `BASELINE COMPARE OK`；gate `GATE: PASS`。

## 回滚

删除 `components/ui/dialog.tsx`、`textarea.tsx`、`components/business/{confirm,detail}-dialog.tsx`、`tests/dialog-usage.test.ts`、`scripts/admin-ui-e2e-dialogs.py`、`docs/impl/evidence/ADM-UI-02c/`；`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/package.json web-admin/package-lock.json scripts/admin-ui-baseline.py`；`npm ci`。
