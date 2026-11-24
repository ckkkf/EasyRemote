# ADM-UI-02b 共享三态组件（Loading / Empty / Error）与视觉回归基线

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-02（行 121-127：共享 Loading/Skeleton、EmptyState、ErrorState）
> 需求：PRD §6.7.1 第 5 条（行 708「所有写操作必须有 loading/成功/失败三态」）、AGENTS §8.9（trace_id 可复制）
> 前置：ADM-UI-01a（primitives）、02a（路由）、03（登录迁移）

## 目标

把每个页面各写一套的加载/空/错误三态收敛成 `components/business/page-state.tsx` 唯一实现：错误态用 shadcn Alert 展示 message + code/name + 可复制 trace_id，并按信封的 `retryable` 决定是否给「重试」；同时建立**可复用的视觉回归基线**（六屏 DOM 判据比对），证明本卡没有改变页面结构。

## 输入 / 输出

不改 API、不改契约、不改生成物，字段映射 **N/A**。

## 必须通过的断言

- [ ] `TestLoadingStateIsAnnounced`（`tests/page-state.test.tsx`）：`role="status"` + `aria-live="polite"`，默认文案「加载中…」，可自定义文案。
- [ ] `TestEmptyStateRendersTextAndHint`（同上）：空态文案与可选 hint 都渲染。
- [ ] `TestErrorStateShowsEnvelopeAndActions`（同上）：`role="alert"`，message/code/name/trace_id 同时可见，且有「复制 trace」「重试」「关闭」。
- [ ] `TestErrorStateRespectsRetryability`（同上）：`retryable:false` 时不给「重试」；无 trace 时不给「复制 trace」；`error=null` 渲染空串。
- [ ] `TestVisualBaselineUnchanged`（`python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-02b/screens --compare-to docs/impl/evidence/ADM-UI-00/screens/manifest.json`）：六屏 DOM 判据与 ADM-UI-00 基线逐项一致，输出 `BASELINE COMPARE OK`。
- [ ] 既有回归全绿：`npx tsc --noEmit`、`npm test -- --run`、`npm run build`、`node scripts/check-web-admin-stack.mjs --build`、`bash scripts/gate.sh`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-02b.md`、`docs/impl/evidence/ADM-UI-02b/**`
- `web-admin/src/components/business/page-state.tsx`（新增）
- `web-admin/src/App.tsx`（删除本地 ErrorBanner/Loading/Empty，改用共享组件；其它逻辑不动）
- `web-admin/src/styles.css`（仅删除 `.banner.error`、`.err-meta`、`.placeholder` 三段已被组件取代的样式；`.banner.ok` 保留）
- `web-admin/tests/page-state.test.tsx`（新增）
- `scripts/admin-ui-baseline.py`（新增 `--out` / `--compare-to` 两种模式；登录页选择器跟随 ADM-UI-03 的迁移）
- `scripts/admin-ui-e2e-routes.py`（仅把 `form.login-form` 选择器换成 `#login-username`）

禁止：改页面列/筛选/请求逻辑、改 `docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`、`server/**`；禁止为通过比对而放宽判据或改基线文件；禁止 `git commit/revert/stash`。

## 验收命令

```bash
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && node scripts/check-web-admin-stack.mjs --build
python3 scripts/admin-ui-baseline.py --out docs/impl/evidence/ADM-UI-02b/screens --compare-to docs/impl/evidence/ADM-UI-00/screens/manifest.json
bash scripts/gate.sh
```

## 完成标准

五条断言全绿；`BASELINE COMPARE OK (6 屏 DOM 判据一致)`；gate `GATE: PASS`；证据落 `docs/impl/evidence/ADM-UI-02b/`。

## 回滚

删除 `web-admin/src/components/business/page-state.tsx`、`web-admin/tests/page-state.test.tsx`、`docs/impl/evidence/ADM-UI-02b/`，`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css scripts/admin-ui-baseline.py scripts/admin-ui-e2e-routes.py`。
