# ADM-UI-00d 最终收尾（矩阵 14/0 + 全量回归）

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md`（ADM-UI-00 收尾）
> 目的：确认 A1-A14 全部有真实实现与证据，全量 E2E 与闸门同时绿，留下可复核的索引。

## 结论

- 矩阵：A1-A14 **14 implemented / 0 partial / 0 blocked**；
- 全量 E2E：`PASS: 14 / FAIL: 0`（13 个脚本 + 视觉基线）；
- 闸门：`GATE: PASS`（scripts / contract / go / rust / flutter / web）。

## 本轮改动

| 文件 | 目的 |
|---|---|
| `scripts/admin-ui-e2e-all.sh` | 视觉基线常量从 `ADM-API-03e` 更新到 `ADM-API-03h`（a7 增加三列后的新基线） |
| `docs/impl/evidence/ADM-UI-00/page-matrix.md` | 表头与变更日志更新到最终状态 |
| `docs/impl/evidence/ADM-UI-00d/**` | 收尾证据（套件、闸门、矩阵、索引） |

## 必须通过的断言

- [ ] `node scripts/check-admin-ui-matrix.mjs`：20 行齐全、证据存在。
- [ ] `bash scripts/admin-ui-e2e-all.sh`：`PASS: 14 / FAIL: 0`。
- [ ] `bash scripts/gate.sh`：`GATE: PASS`。
- [ ] 状态统计：implemented 14 / partial 0。

## 只准改这些文件

- `scripts/admin-ui-e2e-all.sh`
- `docs/impl/evidence/ADM-UI-00/page-matrix.md`
- `docs/impl/tasks/ADM-UI-00d-final-close.md`、`docs/impl/evidence/ADM-UI-00d/**`

禁止：为了让套件变绿而放宽断言；删除缺口说明。

## 验收命令

```bash
node scripts/check-admin-ui-matrix.mjs
bash scripts/admin-ui-e2e-all.sh
bash scripts/gate.sh
```

## 完成标准

四条断言全绿。

## 回滚

`git checkout -- scripts/admin-ui-e2e-all.sh docs/impl/evidence/ADM-UI-00/page-matrix.md`；删除 `docs/impl/evidence/ADM-UI-00d/`。
