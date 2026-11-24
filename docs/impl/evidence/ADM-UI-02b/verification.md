# ADM-UI-02b 验证记录

> 卡：`docs/impl/tasks/ADM-UI-02b.md`
> 时间：2026-11-23 10:44-10:52（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/components/business/page-state.tsx` | 三态唯一实现：`LoadingState`（role=status/aria-live）、`EmptyState`（文案+hint）、`ErrorState`（shadcn Alert + message/code/name/trace + 按 retryable 给重试） |
| `web-admin/src/App.tsx` | 删除本地 `ErrorBanner`/`Loading`/`Empty`，全部页面改用共享组件（30 处调用点）；页面逻辑、列、筛选、请求零改动 |
| `web-admin/src/styles.css` | 删除 `.banner.error`、`.err-meta`、`.placeholder`（已被组件取代）；`.banner.ok` 保留给成功提示 |
| `web-admin/tests/page-state.test.tsx` | 4 条三态断言 |
| `scripts/admin-ui-baseline.py` | 新增 `--out` / `--compare-to`：同一夹具可输出到任意证据目录，并与既有基线逐屏比对 DOM 判据；登录页选择器跟随 ADM-UI-03 |
| `scripts/admin-ui-e2e-routes.py` | 选择器 `form.login-form` → `#login-username`（登录页已迁移） |

## 2. 字段映射

N/A。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 加载态可被读屏感知 | `TestLoadingStateIsAnnounced` | PASS |
| 空态文案 + hint | `TestEmptyStateRendersTextAndHint` | PASS |
| 错误态信封字段 + 三个动作 | `TestErrorStateShowsEnvelopeAndActions` | PASS |
| 不可重试不给重试、无 trace 不给复制、null 不渲染 | `TestErrorStateRespectsRetryability` | PASS |
| 六屏 DOM 判据与基线一致 | `TestVisualBaselineUnchanged` | PASS（`BASELINE COMPARE OK (6 屏 DOM 判据一致)`） |
| 既有回归 | `npm test -- --run` | 8 files / 22 tests PASS |
| 技术栈断言 | `check-web-admin-stack.mjs --build` | 4/4 通过 |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 视觉回归方法（可复用）

`python3 scripts/admin-ui-baseline.py --out <证据目录> --compare-to <基线 manifest>`：

1. 同一套夹具（6 账号 / 10 设备 / 4 会话四态）真实登录后采集六屏；
2. 逐屏比对 `criteria`（列名、行数、指标卡标签、导航项、分页文案、kv 数量、能力台账行数）；
3. 设备码这类随机值在 `modalTitle` 上归一为 `#`，其余不做放宽；
4. 输出 `compare.json`，有差异即非零退出。

本次结果：6 屏全部一致 —— 说明把三态抽成共享组件**没有**改变任何页面的结构、列或行数。

## 5. 未完成项、限制与风险

- 本卡只做三态；对话框（`Modal`/`.modal*`）仍用旧样式，`ConfirmDialog` 与 Radix Dialog 迁移留给 ADM-UI-02c。
- `.pill`/`.kv`/`.detail-h` 等数据展示样式仍是旧层，属后续逐页迁移。
- `ErrorState` 的「重试」依据信封 `retryable`；但页面仍各自决定是否传 `onRetry`（配置页有、列表页无），是否统一重试策略留给 ADM-UI-02c 的查询层卡。
- 视觉基线比对只覆盖正常态；空态/错误态需要专门的夹具（例如让 API 返回 403），属 ADM-UI-10 的 E2E 范围。

## 6. 回滚

删除 `web-admin/src/components/business/page-state.tsx`、`web-admin/tests/page-state.test.tsx`、`docs/impl/evidence/ADM-UI-02b/`；`git checkout -- web-admin/src/App.tsx web-admin/src/styles.css scripts/admin-ui-baseline.py scripts/admin-ui-e2e-routes.py`。
