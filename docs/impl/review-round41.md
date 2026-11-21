# 第四十一轮：闸门转绿（信封迁移 + 测试夹具修正）+ ABR 实测

> 日期：2026-11-20 ｜ 主控 agent 执行

## 一、`bash scripts/gate.sh` 现在全绿

```
== 6/8 禁止模式 ==
无新增违规（存量 5 个文件待迁移）

== 7/8 响应信封契约 ==
ok  github.com/easyremote/server/internal/adminhttp

== 结果 ==
GATE: PASS
```

按 AGENTS.md 第 4 条，默认闸门即严格模式，PASS 才谈得上对齐。

## 二、这次改了什么

### 1. admin 后台响应全部走信封

`server/internal/adminhttp/handler.go`：11 处 `writeJSON(w, 200, X)` → `httpx.OK(w, r, X)`。
成功后业务体进 `data`，`code/name/trace_id` 由信封统一产出（AGENTS.md 铁律 1）。

### 2. 契约测试的会话复用缺陷

`contract_test.go` 原来所有请求共用一个 token，而 `/admin/v1/logout` 会撤销它 → 后续接口全变
`ADMIN_SESSION_EXPIRED`。历史上这些违规被显示成「缺字段 code」，掩盖了真正原因。

改成**每条请求现签一个会话**，问题暴露后自然消失。

### 3. 集成测试不认识信封

`tests/integration` 的 `client.do` 直接读扁平字段，而网关已迁到信封 →
`body["token"].(string)` panic。

修法：`do()` 在响应是成功信封时自动摊平 `data`，错误时保留信封字段。这样测试关注业务字段，
不必每条断言都剥一层。

### 4. 审计接口断言过时

`/admin/v1/audit` 已返回分页 `{items,total}`，测试还在读 `body["audit"]` → nil 断言 panic。
改为读 `items`。

## 三、回归

| 端 | 结果 |
|---|---|
| Go 单测 | 19 包全绿 |
| Go 竞态 | 19 包全绿 |
| 集成测试 | 通过 |
| Rust | **160 passed** |
| Flutter | 18 passed |
| gate.sh | **PASS** |

## 四、ABR 实测（同轮完成）

| 注入丢包 | changes | 最终档位 | 码率 | fps | 输出 |
|---|---|---|---|---|---|
| 0% | 0 | 1080p60 | 8000 | 60 | 2880×1620 (gpu-zero-copy) |
| 15% | 4 | 360p15 | 800 | 15 | 960×540 |

修掉的三个缺陷：queue_ms 把帧间隔当排队、RTT 跨时钟域相减、单周期假丢包即降两档（已加迟滞）。

## 五、仍未闭合

| 项 | 状态 |
|---|---|
| ABR 升档实测 | ⏳ 未测（丢包撤掉后能否爬回） |
| ABR 基于 queue/RTT 降档 | ❌ 需时钟同步，当前按 0 上报 |
| ACCESS_LOST 自愈 | ⚠️ INCONCLUSIVE |
| 禁止模式存量 5 文件 | 🟡 未迁移（不阻塞闸门） |
