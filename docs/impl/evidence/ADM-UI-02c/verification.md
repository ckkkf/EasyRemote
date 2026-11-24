# ADM-UI-02c 验证记录

> 卡：`docs/impl/tasks/ADM-UI-02c.md`
> 时间：2026-11-23 10:52-11:00（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/components/ui/dialog.tsx` | shadcn Dialog 源码（Radix）：Overlay/Content/Header/Footer/Title/Description，焦点陷阱与 Esc 由 Radix 负责 |
| `web-admin/src/components/ui/textarea.tsx` | shadcn Textarea 源码（封禁原因输入） |
| `web-admin/src/components/business/confirm-dialog.tsx` | 二次确认对话框：danger→destructive、`disabled={busy || confirmDisabled}`、关闭统一 `onOpenChange` |
| `web-admin/src/components/business/detail-dialog.tsx` | 详情对话框（设备/用户），内容可滚动 |
| `web-admin/src/App.tsx` | 封禁/中断改用 `ConfirmDialog`；设备/用户详情改用 `DetailDialog`；删除本地 `Modal` 组件 |
| `web-admin/src/styles.css` | 删除 `.modal-mask/.modal/.modal-actions` 等规则；`.modal .kv` 解绑为 `.kv` |
| `web-admin/package.json`、`package-lock.json` | `@radix-ui/react-dialog` |
| `web-admin/tests/dialog-usage.test.ts` | 2 条源码断言（无自绘 modal、危险操作门控） |
| `scripts/admin-ui-e2e-dialogs.py` | 16 条对话框 E2E（真实副作用 + 可访问性） |
| `scripts/admin-ui-baseline.py` | a6 判据改从 `[role=dialog]` 读取 |

## 2. 字段映射

N/A。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 无本地 Modal/旧 class，改引用 Radix Dialog | `TestDialogsUseRadixDialog` | PASS |
| 危险操作 destructive + 禁用门控 + 统一关闭 | `TestConfirmDialogGatesDangerAction` | PASS |
| 对话框可访问性与真实副作用 | `TestDialogE2E` | PASS（16/16） |
| 六屏 DOM 判据不变 | `TestVisualBaselineUnchanged` | PASS |
| 既有回归 / 技术栈 / 闸门 | `npm test` / `stack-check` / `gate.sh` | 9 files / 24 tests；4/4；`GATE: PASS` |

E2E 覆盖的关键语义（不是"弹窗能开"）：

- `role=dialog` + `aria-labelledby` + 外部 `#root` 被 `aria-hidden`（Radix 的模态表达，Radix 不写 `aria-modal`）；
- 打开后焦点落在对话框内；`Esc` 关闭且**不产生任何副作用**（用户仍 active）；
- 原因 0/3 字时「确认封禁」禁用，4 字后启用；确认后用户变 banned 且审计出现 `user.ban`；
- 中断：取消**不写** `session.kill` 审计；确认后写审计；
- 设备详情 6 组 kv；用户详情异步设备表渲染完成。

## 4. 关键输出

```text
Tests  24 passed (24)
汇总: 4/4 通过
E2E OK (16 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 本卡发现的后端缺陷（已建卡，不在本卡修）

**`phone_mask` 恒为 `***`**：`auth.Login`（短信注册）建号时只写 `PhoneHash`，没写 `PhoneEnc`，`adminapi.maskPhone("")` 返回 `***`。复现：`items: [('acc-…', "'***'")]`。这正是 ADM-02-users 缺失的 `TestListUsers_Mask` 应该拦住的问题。已建 `docs/impl/tasks/ADM-02a-user-phone-mask.md`（含根因、断言、历史数据方案待决策）。

## 6. 未完成项、限制与风险

- Radix 用 `aria-hidden` + 焦点陷阱表达模态，不设置 `aria-modal`；E2E 按真实行为断言，不按猜测断言。
- 详情对话框里的 `.kv`/`.pill`/`.detail-h` 仍是旧样式层（数据展示，非对话框 chrome），随逐页迁移处理。
- 中断对话框仍是「原因输入 + 二次确认」；PRD 的「原因类别下拉」「3 秒内断开提示」「双人确认」属 ADM-UI-07，本卡不改交互语义。
- 对话框内容仍各自拉数据（设备/用户详情），迁 `useQuery` 留到查询层卡。

## 7. 回滚

删除 `components/ui/dialog.tsx`、`textarea.tsx`、`components/business/{confirm,detail}-dialog.tsx`、`tests/dialog-usage.test.ts`、`scripts/admin-ui-e2e-dialogs.py`、`docs/impl/evidence/ADM-UI-02c/`；`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css web-admin/package.json web-admin/package-lock.json scripts/admin-ui-baseline.py`；`npm ci`。
