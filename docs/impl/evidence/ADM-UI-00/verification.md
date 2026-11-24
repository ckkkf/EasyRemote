# ADM-UI-00 验收记录

> 卡：`docs/impl/tasks/ADM-UI-00.md`
> 时间：2026-11-23 10:13（Asia/Shanghai）
> 基线 commit：`267a635` chore(release): v0.2.12

## 1. 验收断言 → 测试名 → 结果

| 卡内断言 | 实际执行的命令 / 测试名 | 结果 |
|---|---|---|
| `TestAdminUiMatrixRowsComplete` | `node scripts/check-admin-ui-matrix.mjs` → `PASS TestAdminUiMatrixRowsComplete: 20 行，A1-A14 与 s12-s17 全覆盖` | PASS |
| `TestAdminUiMatrixEvidenceExists` | 同上 → `PASS TestAdminUiMatrixEvidenceExists: 证据路径与 6 张 1360x860 截图均存在` | PASS |
| 负向验证（矩阵不许虚报） | 把 A8 改成 `implemented` 且不留证据 → `FAIL … A8 缺代码文件行号` / `A8 状态为 implemented 但没有任何证据`；改回后 PASS | PASS（断言有效，不是橡皮章） |
| `TestAdminUiBaselineScreenshots` | `python3 scripts/admin-ui-baseline.py` → 6 屏截图 + `BASELINE OK`，`pageErrors: []` | PASS |
| `TestWebAdminBaselineGreen` | `npx tsc --noEmit` exit 0；`npm test -- --run` 3 files / 7 tests 全绿；`npm run build` 成功 | PASS |
| `TestGateBaselineGreen` | `bash scripts/gate.sh` → 20/20 步全 PASS，末行 `GATE: PASS` | PASS |

完整原始输出：`docs/impl/evidence/ADM-UI-00/baseline-commands.txt`（1009 行）。

## 2. 产物清单

| 文件 | 内容 |
|---|---|
| `page-matrix.md` | A1-A14 + X1-X6 共 20 行；每行 spec 行号、原型屏、代码行号、状态、证据、缺口卡、缺口说明 |
| `adm02-check.md` | ADM-02-users 状态核对：后端信封已落地、4 条命名断言缺失、不覆盖不重复执行 |
| `screens/*.png` | 1360×860 六屏基线：a1-login / a2-dashboard / a3-users / a6-device-detail / a7-sessions / a12-config |
| `screens/manifest.json` | viewport、DPR、字体、主题、locale、夹具（6 账号 / 10 设备 / 4 会话四态）、每屏 DOM 判据、pageErrors |
| `baseline-commands.txt` | tsc / vitest / build / go test（ADM-02）/ 矩阵断言 / 截图 / gate 原始输出 |

## 3. 视觉与几何判据（不是"看起来好了"）

- viewport 固定 1360×860、DPR 1、light；字体实测 `Inter, "PingFang SC", "Microsoft YaHei", sans-serif`。
- a2：4 张指标卡，标签 设备总数 / 在线设备 / 进行中会话 / 待授权；2 张表格；6 个导航项。
- a3：列 账号 ID / 手机号 / 状态 / 注册时间 / 操作；6 行；2 个筛选控件；分页「共 6 条」。
- a6：modal 标题「设备详情 826 115 901」；6 组 kv；最近会话 4 行（说明详情确实按设备查了会话）。
- a7：列 会话 ID / 主控 / 被控 / 状态 / 时长 / 开始时间 / 操作；4 行，状态实测 requested / authorized / active / closed。
- a12：配置表 4 行（配置项 / 当前值 / 默认 范围 / 说明）+ 能力台账 7 行。

## 4. 本卡确认的缺陷与缺口（已进矩阵，不属本卡修复范围）

1. **A7 状态枚举错位**：UI 筛选项与禁用条件用 `ended`，状态机终态是 `closed`（`server/internal/session/session.go:19-22`）。截图里 closed 行仍显示可点的「中断」按钮。→ ADM-UI-07。
2. **A1 登录页泄露开发口令**：`App.tsx:255` 展示 `root / dev-pass` 与 `ER_ADMIN_TOTP_OFF` 提示，production build 同样可见。→ ADM-UI-03。
3. **A13 动作选项来自当前页**：`App.tsx:925` 用 `new Set(rows.map(...))` 生成下拉，不是服务端合法值集合。→ ADM-UI-08。
4. **X6 E2E 脚本漂移**：`scripts/verify-admin-ui.py` 仍写 `localStorage`，而 `src/api.ts` 已用 `sessionStorage`，直接跑会停在登录页。→ ADM-UI-10。
5. **ADM-02-users 断言未齐**：`TestListUsers_Envelope/_Mask/_Empty/_Forbidden` 不存在，需由该卡负责人补。见 `adm02-check.md`。
6. **A2/A6/A7/A12/A13/A14 缺字段或接口**：质量与稳定性指标、30 天设备曲线、会话延迟/链路、配置历史与回滚、审计服务端选项、管理员与角色，均须先开 contract-first 后端卡。

## 5. 工作区与副作用声明

- 本轮**没有** `git commit / revert / stash`，没有改 `web-admin/src/**`、`server/**`、`docs/spec/**`、`docs/prd/**`、`prototype/**`。
- 开工时（10:01）工作区有 ER-903 ratelimit 与 `vite.demo.config.ts` 等未提交改动；10:04-10:05 由外部进程提交为 `674866a` / `267a635`（含 VERSION 0.2.12），内容全部保留，未丢失、未覆盖。
- `core/Cargo.lock` 现为 `M`：gate 跑 cargo 时把 workspace 版本从 0.2.11 同步到 0.2.12（与 `VERSION`/`Cargo.toml` 一致）。这是版本一致性副产物，保留不回滚。

## 6. 未完成项

- 本卡无未完成断言。
- 六屏之外的页面（A4/A5/A8/A9/A10/A11/A13/A14）暂无截图基线，按计划由 ADM-UI-05/06/07/08/09/10 各自补。

## 7. 回滚

删除 `docs/impl/tasks/ADM-UI-00.md`、`docs/impl/evidence/ADM-UI-00/`、`scripts/check-admin-ui-matrix.mjs`、`scripts/admin-ui-baseline.py` 即回到基线；不涉及任何既有实现的回滚。
