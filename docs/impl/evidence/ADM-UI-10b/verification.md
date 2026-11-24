# ADM-UI-10b 验证记录

> 卡：`docs/impl/tasks/ADM-UI-10b.md`
> 时间：2026-11-23 11:50-12:02（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `web-admin/src/features/sessions/kill-result.ts` | 中断结果三态（ok/partial/error）的纯函数判定 |
| `web-admin/src/App.tsx` | Sessions.kill 改用 `describeKillResult`（行为不变，语义集中） |
| `web-admin/tests/kill-result.test.ts` | 4 条断言，覆盖本地无法注入的 partial 分支 |
| `scripts/admin-ui-e2e-lifecycle.py` | 11 条真实 E2E：分页翻页 + 服务端登出 + 未登录深链 |

## 2. 字段映射

N/A。

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 三者全真才是成功 | `TestKillResultOkRequiresClosedPropagatedAudited` | PASS |
| 传播失败 → partial 且不说"均已确认" | `TestKillResultPartialOnPropagationFailure` | PASS |
| 审计失败 → partial | `TestKillResultPartialOnAuditFailure` | PASS |
| 未关闭 → error | `TestKillResultErrorWhenNotClosed` | PASS |
| 分页/登出/深链 | `TestLifecycleE2E` | PASS 11/11 |
| 回归/技术栈/闸门 | `npm test`（54）/ `stack-check` 4/4 / `gate.sh` | 全绿 |

## 4. 关键输出

```text
Tests  54 passed (54)
PASS page 1 has page size rows -> 10
PASS pager reports 12 total / 2 pages -> 共 12 条 · 第 1 / 2 页
PASS page 2 has remaining rows -> 2
PASS page 2 rows differ from page 1 -> ['acc-…','acc-…']
PASS back to page 1 restores rows -> 10
PASS token valid before logout -> 200 OK
PASS logout lands on login -> http://127.0.0.1:5403/login
PASS logout clears stored token
PASS server session revoked after logout -> 401 ADMIN_SESSION_EXPIRED
PASS anonymous deep link to login
E2E OK (11 checks)
GATE: PASS
```

截图：`users-page1.png`、`users-page2.png`、`after-logout.png`。

## 5. 过程中发现并修掉的测试缺陷（不是产品缺陷）

- 分页断言最初只等"分页文案"，会出现"文案已变、行数据还是上一帧"的竞态 → 改为等行数恢复后再比对，并用 `expect_response` 等真实 `offset=10` 请求。
- 登出后复用同一个 page 去访问深链会读到旧状态 → 改为新上下文验证守卫本身。
- 夹具要 12 个账号，触发网关 `sms:ip` 10 次/小时限流（429）→ 该用例只验分页/登出，显式用 `ER_RATELIMIT_OFF=1`（生产配置会启动失败），并在脚本注释里写明理由。

## 6. 未完成项、限制与风险

- **partial 仍无端到端用例**：本地单进程 stack 无法让 `propagated=false`/`audited=false`；需要服务端故障注入钩子（测试专用）或双进程 + 断链场景，属后续卡。当前用纯函数单测锁死语义。
- 登出只验证了 admin session 撤销（ER-811 语义）；令牌存储仍是 sessionStorage，威胁模型未变。
- 分页只覆盖用户列表；设备/会话/审计分页复用同一 `Pagination` 组件与 `limit/offset` 路径，未逐个跑翻页。

## 7. 回滚

删除 `kill-result.ts`、`tests/kill-result.test.ts`、`scripts/admin-ui-e2e-lifecycle.py`、`docs/impl/evidence/ADM-UI-10b/`；`git checkout -- web-admin/src/App.tsx`。
