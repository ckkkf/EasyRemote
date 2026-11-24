# ADM-API-02c 验证记录（会话质量采样）

> 卡：`docs/impl/tasks/ADM-API-02c-session-quality.md`
> 时间：2026-11-23 12:56-13:06（Asia/Shanghai）

## 1. 改动文件及目的

| 文件 | 目的 |
|---|---|
| `docs/spec/contract.yaml` | `QualitySample`/`QualityReportRequest`/`SessionQualityResult` + GW-26 + ADM-17 |
| 三端生成物、两端 `route_specs.go` | 生成 + 声明 |
| `migrations/005_session_quality.sql` | 采样表（主键 session_id+ts，同秒幂等） |
| `store/{store,memory,pg}.go`、`storetest` | `AppendQualitySamples` / `ListQualitySamples` |
| `gateway/handler.go` | `POST /v1/sessions/quality`：设备鉴权 + 会话双方校验 + 截断到秒 |
| `adminapi/adminapi.go`、`quality_test.go` | `SessionQuality`（available 标记） |
| `adminhttp/handler.go` | `GET /admin/v1/sessions/quality` |
| `web-admin` | A8 页面：真实采样表 + 平均延迟/丢包/帧率/中继占比；无采样明确提示 |
| `scripts/admin-ui-e2e-sessions.py` | 21 条 E2E（含真实设备上报） |

## 2. 字段映射

| 端点 | 请求 | 响应 |
|---|---|---|
| GW-26 `POST /v1/sessions/quality` | `{session_id, samples:[{ts,latency_ms,loss_permille,fps,tier,relay}]}` | `OkResult` |
| ADM-17 `GET /admin/v1/sessions/quality?session_id=` | — | `{session_id, available, samples}` |

## 3. 断言 → 测试名 → 结果

| 断言 | 测试名 | 结果 |
|---|---|---|
| 契约/路由一致 | `gen-contract --check` / `check-routes --strict` | PASS（45 端点） |
| 同秒覆盖、排序、空会话 | `TestMemoryContract/QualitySamples` | PASS |
| available 语义 + 权限 | `TestSessionQualityAvailableFlag` | PASS |
| 端到端（真实上报 → 页面渲染） | `TestSessionsE2E` | PASS 21/21 |
| 六屏判据 | `TestVisualBaselineUnchanged` | PASS |
| 闸门 | `bash scripts/gate.sh` | `GATE: PASS` |

## 4. 关键输出

```text
✓ 三端契约一致（46 个类型 / 45 条接口）
✓ 路由 method/path/auth 与契约一致（45 条端点）
--- PASS: TestMemoryContract/QualitySamples
--- PASS: TestSessionQualityAvailableFlag
PASS quality samples rendered -> 3
PASS quality summary computed
PASS quality link column shows relay
E2E OK (21 checks)
BASELINE COMPARE OK (6 屏 DOM 判据一致)
GATE: PASS
```

## 5. 过程中修掉的问题

- **信封测试与 ADM-17 冲突**：`TestAdminEnvelopeContract` 要求所有 GET+admin 返回 200，而最初空 `session_id` 返回错误。改为"空/未知会话返回 `available=false` + 空数组"——语义更准确（区分"没数据"和"查询失败"），也不再破坏信封契约。
- 白名单测试再次拦住新增端点，同步 `admin-scope.test.ts` 后通过。

## 6. 未完成项、限制与风险

- 采样只按会话存储，没有按日聚合；设备 30 天曲线、仪表盘质量/稳定性指标、趋势图仍缺（需要聚合任务与图表库）。
- 客户端（Flutter/core）尚未上报；真机会话目前显示「没有采样数据」。服务端链路已用设备凭证端到端验证。
- 采样无 TTL/清理策略，长期会增长；需要保留期与归档卡。
- 采样表未按会话做权限隔离（管理端只读、session.read 即可），与现有审计/列表一致。

## 7. 回滚

`git checkout --` 契约/生成物/route_specs/store/gateway/adminapi/handler/前端/E2E；删除 005 迁移与 `docs/impl/evidence/ADM-API-02c/`。
