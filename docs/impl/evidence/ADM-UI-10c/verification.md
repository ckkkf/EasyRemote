# ADM-UI-10c 验证记录（E2E 一键回归）

> 卡：`docs/impl/tasks/ADM-UI-10c-e2e-suite.md`
> 时间：2026-11-23 16:18-17:56（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `scripts/admin-ui-e2e-all.sh`（新增） | 顺序跑 13 个 E2E + 基线比对，日志落盘，汇总 PASS/FAIL |
| `scripts/admin-ui-e2e-login.py` | 指标卡断言从"恰好 4 张"改为 ≥4 且三组齐全 |
| `scripts/admin-ui-e2e-admins.py` | 管理员数量断言 2 → ≥3（seed 增加 ops2-dev） |
| `scripts/admin-ui-e2e-dialogs.py` | 精确 role 名、`#kill-detail`、设备/用户详情改为断言路由跳转 |
| `scripts/admin-ui-responsive-a11y.py` | 溢出失败时打印越界元素（诊断） |
| `web-admin/src/components/business/data-table.tsx` | 表格外包 `.tbl-wrap` 横向滚动容器 |
| `web-admin/src/styles.css` | `.main/.card` 加 `min-width:0`；单列 `.shell` 用 `minmax(0,1fr)`；`.tbl-wrap` |

## 2. 断言 → 结果

| 断言 | 结果 |
|---|---|
| 全量套件 | `PASS: 14 / FAIL: 0`，`E2E SUITE: PASS`，耗时 143s |
| 失败可被发现 | 首轮实测 3 个失败并退出码 1（非静默通过） |
| web 单测 | 57/57 PASS |
| 闸门 | `GATE: PASS` |

## 3. 关键输出

```text
== 汇总 ==
  PASS: 14
  FAIL: 0
  耗时: 143s
  E2E SUITE: PASS

AUDIT OK        （响应式审计）
BASELINE OK     （六屏判据）
GATE: PASS
```

## 4. 本轮修掉的真实回归（由套件首次跑出）

1. `login`：断言"恰好 4 张指标卡"——仪表盘早已扩到 15 张，属于**测试债**（不是产品缺陷）。
2. `admins`：断言"恰好 2 个管理员"——开发 seed 增加了 `ops2-dev`。
3. `dialogs`：①`has_text="中断"` 同时命中「双人中断」；②字段 id 早已从 `#kill-reason` 改为 `#kill-detail`；③设备/用户详情已从对话框改为独立路由。
4. `responsive`：`/users`、`/devices` 在 1024/768 横向溢出——**真实产品缺陷**：网格子项 `min-width:auto` 让宽表格把整页顶出视口，表格也没有滚动容器。已修 CSS + DataTable 包装，审计从 4 findings → `AUDIT OK`。

## 5. 未完成项、限制与风险

- 套件依赖本机 Chrome（Playwright `channel="chrome"`）与 Python 依赖；CI 需要预装。
- 不进 gate（约 2.5 分钟 + 浏览器），发版前手动跑；如果后续 CI 有浏览器环境，可作为独立 job。
- 基线比对固定对比 `ADM-API-03e` 的 manifest；视觉有意变更后要同步更新该常量（脚本里已注明）。
- 每个脚本固定端口（5394-5404、8299-8302），并行执行会冲突；脚本内是顺序跑。

## 6. 回滚

删除 `scripts/admin-ui-e2e-all.sh`；`git checkout --` 四个 E2E 脚本、`data-table.tsx`、`styles.css`；删除 `docs/impl/evidence/ADM-UI-10c/`。
