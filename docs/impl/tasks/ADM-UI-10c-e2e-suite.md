# ADM-UI-10c 管理端 E2E 一键回归

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-10 收口）
> 目的：把 12 个 E2E 脚本 + 视觉基线串成一条可复现命令，避免"手敲漏跑"。

## 目标

1. 新增 `scripts/admin-ui-e2e-all.sh`：顺序执行全部 E2E（端口不冲突）→ 每个脚本日志落盘 → 汇总 PASS/FAIL；
2. 顺带暴露并修掉长期未发现的 3 处真实回归（见下）；
3. 说明为何**不进 gate.sh**：全量约 2.5 分钟且依赖浏览器，gate 保持 20 步快速闸门；发版前/大改 UI 后手动跑。

## 必须通过的断言

- [ ] `bash scripts/admin-ui-e2e-all.sh`：`PASS: 14 / FAIL: 0`（13 个脚本 + 基线比对）。
- [ ] 失败时脚本退出码非 0，并打印失败脚本名（已实测：首轮 3 个失败）。
- [ ] `bash scripts/gate.sh` 全绿。

## 本轮修掉的真实回归

| 失败 | 根因 | 修复 |
|---|---|---|
| `admin-ui-e2e-login` | 断言"恰好 4 张指标卡"，但仪表盘已扩到 15 张 | 改为 ≥4 且三组齐全 |
| `admin-ui-e2e-admins` | 断言"恰好 2 个管理员"，seed 已加 `ops2-dev` | 改为 ≥3 |
| `admin-ui-e2e-dialogs` | ①`has_text="中断"` 同时命中「双人中断」；②`#kill-reason` 字段早已改名 `#kill-detail`；③设备/用户详情已从对话框改成独立路由 | 精确 role 名、改字段 id、改为断言路由跳转 |
| `admin-ui-responsive-a11y` | `/users`、`/devices` 在 1024/768 横向溢出（网格子项 `min-width:auto` + 表格无滚动容器） | `.main/.card` 加 `min-width:0`、`.shell` 单列用 `minmax(0,1fr)`、DataTable 外层 `.tbl-wrap{overflow-x:auto}` |

## 只准改这些文件

- `scripts/admin-ui-e2e-all.sh`（新增）、`scripts/admin-ui-e2e-{login,admins,dialogs}.py`、`scripts/admin-ui-responsive-a11y.py`（诊断输出）
- `web-admin/src/components/business/data-table.tsx`、`web-admin/src/styles.css`
- `docs/impl/tasks/ADM-UI-10c-e2e-suite.md`、`docs/impl/evidence/ADM-UI-10c/**`

禁止：为了让套件通过而删除断言、放宽阈值或跳过脚本。

## 验收命令

```bash
bash scripts/admin-ui-e2e-all.sh
cd web-admin && npx tsc --noEmit && npm test -- --run
cd .. && bash scripts/gate.sh
```

## 完成标准

三条断言全绿；套件 `E2E SUITE: PASS`；gate `GATE: PASS`。

## 回滚

删除 `scripts/admin-ui-e2e-all.sh`；`git checkout --` 三个 E2E 脚本、responsive 脚本、data-table.tsx、styles.css；删除 `docs/impl/evidence/ADM-UI-10c/`。
