# GitHub 基础设施与工作流

本目录定义了 EasyRemote 的自动化规范。所有工作流与 `scripts/gate.sh` 共用同一套判断标准——**CI 红 == 本地闸门红**。

---

## 一、工作流一览

| 文件 | 触发 | 作用 |
|---|---|---|
| [`workflows/ci.yml`](workflows/ci.yml) | push main / PR / 手动 | 全量构建与测试：契约闸门、Go、Rust、React、Flutter 五个 job |
| [`workflows/pr.yml`](workflows/pr.yml) | PR 打开/编辑/更新 | PR 标题语义化、改动规模提醒、自查清单、自动打标签 |
| [`workflows/tag.yml`](workflows/tag.yml) | push 任意 Tag | Tag 必须符合语义化版本，且与包版本一致、位于 main 历史 |
| [`workflows/release.yml`](workflows/release.yml) | push `v*.*.*` | 跨平台打包并发布 GitHub Release（附 SHA256SUMS） |
| [`workflows/codeql.yml`](workflows/codeql.yml) | push / PR / 每周一 | Go 与 JS/TS 静态安全分析 |

辅助文件：[`dependabot.yml`](dependabot.yml)（依赖更新）、[`labeler.yml`](labeler.yml)（路径→标签映射）、[`CODEOWNERS`](CODEOWNERS)、Issue/PR 模板。

---

## 二、CI 校验项

`ci.yml` 的 **契约与规范闸门** job 与 `scripts/gate.sh` 保持一致：

1. `node scripts/gen-errors.mjs --check` — 错误码生成物与文档一致
2. `node scripts/gen-contract.mjs --check` — 三端契约生成物一致
3. `node scripts/check-routes.mjs --strict` — 路由清单与 `contract.yaml` 一致
4. `gofmt -l server` — 格式
5. `go vet ./...`
6. 禁止模式 — 不允许绕过 `httpx` 直接写响应（`json.NewEncoder` / `http.Error` / `w.Write([]byte`）

其余 job：Go 构建 7 个入口 + `-race` 测试；Rust `fmt` / `clippy -D warnings` / `test`；React `tsc` + `build`；Flutter `analyze` + `test`。

---

## 三、PR 规范

### 标题格式

```
<type>(<scope>): <subject>
```

- **type**（必填）：`feat` `fix` `docs` `style` `refactor` `perf` `test` `build` `ci` `chore` `revert`
- **scope**（可选）：`server` `core` `client` `web-admin` `docs` `spec` `scripts` `ci` `deps`
- **subject**：中文或英文均可，但**不能以大写字母开头**

```
✅ feat(server): 新增设备绑定接口
✅ fix(core): 修复 NVENC 在 4K 下的花屏
❌ Feat: 新增接口            ← type 小写
❌ feat(server): 新增接口。   ← 不以句号结尾
❌ update code                ← 缺少 type
```

### 自查清单

PR 描述模板中的四项必须勾选，否则 `pr.yml` 会失败并在 PR 里留评论提示。

### 自动标签

按改动路径自动打 `server` `core` `client` `web-admin` `spec` `docs` `ci` `dependencies`。

---

## 四、版本与 Tag 规范

### 版本单一来源

版本号散落在四处，由 `scripts/check-version.mjs` 统一：

| 文件 | 字段 |
|---|---|
| `VERSION` | 纯文本版本号（唯一权威） |
| `core/Cargo.toml` | `[workspace.package] version` |
| `web-admin/package.json` | `version` |
| `client/pubspec.yaml` | `version`（可带 `+build` 后缀） |

```bash
node scripts/check-version.mjs                # 查看并校验一致性
node scripts/check-version.mjs --set 0.2.0    # 四处同时改成 0.2.0
node scripts/check-version.mjs --tag v0.2.0   # 校验 Tag 与包版本一致
```

### Tag 格式

**必须**是 `vX.Y.Z` 或 `vX.Y.Z-<prerelease>`：

| | 示例 |
|---|---|
| ✅ | `v1.0.0` `v0.2.1` `v1.0.0-rc.1` |
| ❌ | `1.0.0`（缺 v）`v1.0`（缺 patch）`v1.0.0.0`（四段）`release-1.0` |

Tag 还必须：**与包版本一致**、**指向 main 分支历史中的提交**。三条任一不满足，`tag.yml` 即红。

### 发布流程

```bash
# 1. 升版本（四处同步）
node scripts/check-version.mjs --set 0.3.0

# 2. 过闸门
bash scripts/gate.sh

# 3. 提交
git add -A && git commit -m "chore(release): v0.3.0"
git push origin main

# 4. 打 Tag 并推送（自动触发 release.yml）
git tag v0.3.0
git push origin v0.3.0
```

---

## 五、Release 产物

`release.yml` 在 Tag 推送后产出：

| 产物 | 内容 |
|---|---|
| `easyremote-<ver>-<os>-<arch>.tar.gz` | Go 七个入口（admin/gateway/relay/rendezvous/stack/udptun/lossyproxy） |
| `easyremote-core-<ver>-<target>` | Rust `er-ffi` cdylib（Linux / macOS arm64 / Windows） |
| `easyremote-web-admin-<ver>.tar.gz` | 运营后台静态站点 |
| `SHA256SUMS.txt` | 全部产物的校验和 |

架构矩阵：linux/amd64、linux/arm64、darwin/arm64、darwin/amd64、windows/amd64。

带 `-rc` / `-beta` 的 Tag 会自动标记为 **Pre-release**。

---

## 六、维护者清单

仓库设置里需要（一次性，需 admin 权限）：

- [ ] **Actions** → General → Workflow permissions 选 **Read and write permissions**（Release 需要）
- [ ] **Settings → Branches** 为 `main` 加保护：要求 CI 通过、要求 PR 审查
- [ ] **Settings → Secrets** 按需配置（当前工作流只用 `GITHUB_TOKEN`，无需额外密钥）
- [ ] 确认 `CODEOWNERS` 中的所有者账号正确
