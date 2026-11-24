# ADM-02-users 核对（ADM-UI-00 前置检查）

> 核对时间：2026-11-23
> 卡：`docs/impl/tasks/ADM-02-users.md`
> 计划要求：`docs/impl/web-admin-redesign-plan-v0.1.md:63` —— ADM-UI-00 必须先核对该卡状态与证据，**不得覆盖、改写或重复执行**它限定的后端改动。

## 1. 卡的实际范围

ADM-02-users 只做一件事：`GET /admin/v1/users` 从 `{"users":[...]}` 改成统一信封 `{code,name,data,trace_id}`，数据进 `data.items`，并新增 5 条断言。白名单仅 `adminapi.go` / `adminhttp/handler.go` / `adminapi_test.go` / `web-admin/src/api.ts` / `prd-traceability.md`。

它**不包含**用户页面的列、筛选、操作、视觉迁移 —— 那些属于 ADM-UI-05。

## 2. 当前状态：后端信封已落地，卡内命名断言未齐

| 卡内断言 | 现状 | 证据 |
|---|---|---|
| `TestAdminEnvelopeContract`：ADM-02 不再出现在违规列表 | **通过** | `cd server && go test ./internal/adminhttp/ -run Envelope -v` → `全部 38 条接口响应形状已对齐` / `PASS` |
| `TestListUsers_Envelope` | **不存在** | `grep -rn TestListUsers_ server/` 无匹配 |
| `TestListUsers_Mask` | **不存在** | 同上 |
| `TestListUsers_Empty` | **不存在** | 同上 |
| `TestListUsers_Forbidden` | **不存在** | 同上 |

实现侧：

- `server/internal/adminhttp/handler.go:129-142` 已用 `httpx.OKPage` 输出 `{items,total}` 信封。
- `web-admin/src/api.ts:215-216` 已按 `body.data.items` 读取。
- `docs/spec/contract.yaml:241-245` 的 `ADM-04` 是 `GET /admin/v1/users`（注意：契约里的 ADM-02 是 `POST /admin/v1/logout`，卡号与契约号并不一一对应，属历史编号漂移）。

结论：**功能上不再阻塞 ADM-UI-05**（信封与 `data.items` 已生效，38 条路由信封对齐），但该卡按 `AGENTS.md` §13 仍不能算"已完成"——5 条断言里 4 条没有对应测试名。

## 3. 本次处置

1. 不改、不合并、不重复执行 ADM-02-users 的后端改动；不重写它的白名单文件。
2. 记录缺口：ADM-02-users 仍缺 4 条命名断言，需要由该卡负责人补测或重开该卡。**本卡不代做**（否则就是"顺手修复"扩大范围）。
3. ADM-UI-05 开工前置条件：信封契约已满足，可推进；但用户页面新增列（昵称/设备数/最近登录）需要 contract-first 后端卡，不能在前端造字段。

## 4. 复现命令

```bash
cd server && go test ./internal/adminapi/ ./internal/adminhttp/ -run 'ListUsers|Envelope' -v
grep -rn 'TestListUsers_' server/ || echo 'NO NAMED ASSERTIONS'
```
