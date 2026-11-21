# 响应信封收口（Envelope Rollout）

> 日期：2026-11-20 ｜ 范围：server 全部 HTTP 路由 + web-admin + Flutter 客户端
> 契约：[docs/spec/contract.yaml](../spec/contract.yaml) ｜ 闸门：`scripts/gate.sh`

## 1. 结果

| 面 | 收口前 | 收口后 |
|---|---|---|
| 成功响应走信封 | 2 / 31 条路由 | **31 / 31** |
| 404 兜底 | text/plain | JSON 信封（700011） |
| 路由 ↔ 契约 | 21 条未登记 | 0 条（`check-routes.mjs` 校验） |
| web-admin 解析 | 读裸字段 | 读 `data` |
| Flutter 解析 | `body["error"]["code"]`（永远 UNKNOWN） | 读扁平信封 + `data` |
| gate 默认模式 | 报告（不判红） | **严格（红即失败）** |

## 2. 改了什么

**服务端**
- `server/internal/httpx/envelope_ok.go`：`OK` / `OKPage` / `OKStatus` / `Page[T]` / `TraceOf` / `JSONMux`。
- 成功体统一为 `{code:0, name:"OK", data, trace_id}`；错误体 `{code, name, message, retryable, trace_id}`。
- 后台 11 条、网关 20 条路由全部改走信封；删掉 `gateway.WriteJSON` 旧壳，绕不过去了。
- `adminapi` 的列表接口从 `map[string]any` 改为生成的 `contract.*` 类型。
- `cmd/rendezvous` 的 `/healthz` 也改走 `httpx.OK`。

**契约与生成**
- `docs/spec/contract.yaml` 升级到 v2：10 个类型、31 条接口、支持 `map<string>` / `any` / `int64`。
- `scripts/gen-contract.mjs` 生成 Go / TS / Dart 三端类型 + 接口清单；`--check` 检测漂移。
- `scripts/check-routes.mjs` 比对「代码注册的路由」与「契约声明的接口」，两边不对称就报。

**闸门**
- `scripts/gate.sh` 八步：gofmt → vet+test → tsc → 生成物一致 → 路由清单 → 禁止模式 → 信封契约 → 结论。
- 默认严格模式；`--report` 只列待办；`--update-baseline` 更新禁止模式基线。

**客户端**
- `web-admin/src/api.ts`：成功响应解 `data`，列表改读 `items`。
- `client/lib/api.dart`：错误读扁平信封（`code/name/message`），成功解 `data`。

## 3. 证据

```
GET /healthz                200 {"code":0,"name":"OK","data":{"ok":true},"trace_id":"..."}
GET /admin/v1/users         200 {"code":0,"name":"OK","data":{"items":[],"total":0},"trace_id":"..."}
GET /admin/v1/stats/overview 200 {"code":0,"name":"OK","data":{"devices_total":0,...},"trace_id":"..."}
GET /admin/v1/nope          404 {"code":700011,"name":"SRV_ROUTE_NOT_FOUND","message":"请求的接口不存在",...}
```

```
$ bash scripts/gate.sh
== 5/8 路由清单 vs 契约 ==   ✓ 路由清单与契约一致（30 条）
== 6/8 禁止模式 ==            无新增违规
== 7/8 响应信封契约 ==        ok
GATE: PASS
```

## 4. 还没做完的（下一轮）

1. **网关 19 条 payload 仍是 `any`**：信封已统一，但业务体类型还没逐个定型。做法：每次改到某端点时，在 contract.yaml 把 `any` 换成具体类型，跑 gen-contract，编译期就会逼着改代码。
2. **禁止模式基线里还有 5 个文件**（`map[string]any` 作为 payload）：等第 1 条推进后自然归零。
3. **rendezvous / relay 的 HTTP 面**还没纳入 `check-routes.mjs`（目前只扫 adminhttp + gateway）。
4. **视觉回归**：原型 → 截图基线还没接（`docs/spec/pages.md` 需要补字段表）。

## 5. 回归方式

```bash
bash scripts/gate.sh                 # 必须 PASS
bash scripts/gate.sh --report        # 看还有多少待办
node scripts/check-routes.mjs        # 路由/契约是否对称
node scripts/gen-contract.mjs --check
```
