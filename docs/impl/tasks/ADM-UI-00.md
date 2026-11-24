# ADM-UI-00 基线与页面验收清单

> 来源：`docs/impl/web-admin-redesign-plan-v0.1.md` §6 ADM-UI-00（行 106-111）、§8（行 205-215）
> 需求基线：`docs/prd/easyremote-prd-v0.8.md` §6（行 543-790）、`docs/spec/pages.md` A1-A14（行 197-272）、`prototype/index.html` s12-s17（行 1924-2530）
> 性质：**只读基线卡**。不写业务代码、不改接口、不改 spec、不改 web-admin 源码。

## 目标

建立管理端「原型 6 屏 × 页面 spec A1-A14 × 当前实现」的可追踪矩阵与可复现基线证据，给每一项标 `implemented / partial / missing / blocked-by-api / blocked-by-security`，并证明当前 admin 前端测试/构建/闸门状态。

## 输入 / 输出

本卡不新增、不修改任何 API，字段映射 **N/A**（每一行用 `pages.md` 行号与 `PRD` 行号代替字段来源）。

产出：

| 产物 | 路径 | 说明 |
|---|---|---|
| 页面矩阵 | `docs/impl/evidence/ADM-UI-00/page-matrix.md` | A1-A14 + 6 原型屏 + 横向技术栈行；每行含 spec 行号、代码行号、状态、证据、缺口卡 |
| 基线命令输出 | `docs/impl/evidence/ADM-UI-00/baseline-commands.txt` | tsc / vitest / build / gate 原始输出 |
| 六屏截图 | `docs/impl/evidence/ADM-UI-00/screens/*.png` | 1360×860，A1/A2/A3/A6/A7/A12 |
| 截图清单 | `docs/impl/evidence/ADM-UI-00/screens/manifest.json` | viewport、字体、主题、fixture、DOM 判据、console error |
| ADM-02 核对 | `docs/impl/evidence/ADM-UI-00/adm02-check.md` | 现有卡状态、已实现部分、缺失断言，声明不覆盖不重复执行 |

## 必须通过的断言

- [ ] `TestAdminUiMatrixRowsComplete`（`node scripts/check-admin-ui-matrix.mjs`）：A1-A14 各出现一次；6 个原型屏 s12-s17 全覆盖；每行 spec 引用可解析为 `pages.md:<行号>` 且 `PRD:<行号>`；状态取值合法；`implemented` 行必须有证据；`missing` 行必须有缺口卡。
- [ ] `TestAdminUiMatrixEvidenceExists`（同上脚本）：证据里出现的文件路径必须真实存在；截图必须在 1360×860 且 6 张齐全。
- [ ] `TestAdminUiBaselineScreenshots`（`python3 scripts/admin-ui-baseline.py`）：真实 stack + 真实 admin API 登录后采集 6 屏，viewport 1360×860，`pageerror` 为 0，输出 `manifest.json` 并打印 `BASELINE OK`。
- [ ] `TestWebAdminBaselineGreen`：`npx tsc --noEmit` 0 error；`npm test -- --run` 全绿；`npm run build` 成功。
- [ ] `TestGateBaselineGreen`：`bash scripts/gate.sh` 末行 `GATE: PASS`。

## 只准改这些文件

- `docs/impl/tasks/ADM-UI-00.md`（本卡）
- `docs/impl/evidence/ADM-UI-00/**`（矩阵、命令输出、截图、ADM-02 核对）
- `scripts/check-admin-ui-matrix.mjs`（矩阵断言脚本）
- `scripts/admin-ui-baseline.py`（截图基线脚本，测试夹具，不打包进 release）

禁止：改 `web-admin/src/**`、`server/**`、`docs/spec/**`、`docs/prd/**`、`prototype/**`、`design/generated/**`；禁止 `git commit / revert / stash`；禁止把原型示例数据写进证据当作已实现功能。

## 验收命令

```bash
node scripts/check-admin-ui-matrix.mjs
python3 scripts/admin-ui-baseline.py
cd web-admin && npx tsc --noEmit && npm test -- --run && npm run build
cd .. && bash scripts/gate.sh
```

## 完成标准

五条断言全绿 + 矩阵每行都有 spec 行号/代码行号/状态/证据/缺口卡 + 6 屏截图与 manifest 落盘 + gate `GATE: PASS`；回复贴出命令输出与截图路径。

## 回滚

本卡只新增 `docs/impl/evidence/ADM-UI-00/` 与两个脚本，删除这三个新增路径即回到基线，不触碰任何既有实现。
