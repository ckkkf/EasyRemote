# 任务卡（Task Card）

一张卡 = 一次可交付的最小改动。**AI 一次只做一张卡**，做完贴 gate 输出。

## 为什么要有卡

散文式需求（「按 PRD 落地后台」）会让模型自由发挥；卡片把「做什么 / 字段长什么样 / 怎么算做完」压到 100 行以内，模型不需要读 1272 行 PRD 也不会跑偏。

## 卡片格式（缺一节就不算卡）

1. **目标**：一句话，动词开头。
2. **输入/输出**：字段表，每个字段标 `spec 行号`。字段表由 `docs/spec/contract.yaml` 生成，禁止手写新字段。
3. **必须通过的断言**：可执行，带测试函数名。
4. **只准改这些文件**：白名单，超出即打回。
5. **验收命令**：复制粘贴就能跑。

## 信封收口顺序（--strict 之前必须做完）

| 顺序 | 卡片 | 端点 | 客户端影响 |
|---|---|---|---|
| 1 | ADM-02-users | GET /admin/v1/users | web-admin |
| 2 | ADM-01-overview | GET /admin/v1/stats/overview | web-admin |
| 3 | ADM-04-devices | GET /admin/v1/devices | web-admin |
| 4 | ADM-05-sessions | GET /admin/v1/sessions | web-admin |
| 5 | ADM-07-audit | GET /admin/v1/audit | web-admin |
| 6 | ADM-03-09-writes | ban / kill / logout | web-admin |
| 7 | GW-* | /v1/* 全量 | Flutter + web-admin |
| 8 | 收口 | `bash scripts/gate.sh --strict` 全绿 | — |

## 规则

- 卡片里的字段表与 `docs/spec/contract.yaml` 不一致时，先改 contract.yaml 再改卡。
- 卡片没写的字段，不许出现在响应里。
- 完成 = 断言全绿 + gate 输出贴在回复里。
