# ER-800 证据：后台 presence

- 状态: 完成（在线数/列表）
- gate: PASS

## 1. 变更

| 文件 | 内容 |
|---|---|
| adminapi | Presence 接口、SetPresence、Overview/ListDevices 使用 |
| stack/cmd | BuildAdmin 注入；Redis/PG 装配 presence |
| test | TestAdminOnlineUsesPresence |

## 2. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| presence=false 时设备表 true 也不计在线 | TestAdminOnlineUsesPresence | PASS |
| presence=true 时计在线 | 同上 | PASS |
| 设备列表 online 同源 | 同上 | PASS |
| 严格闸门 | bash scripts/gate.sh | GATE: PASS |

## 3. 未完成

- 后台质量统计（FPS/码率/丢包）仍无数据面来源。
- 概览固定取前 500 设备/固定分页上限，需存储层分页（ER-801）。
- presence 未注入时仍回落 d.Online（开发兜底），生产必须注入。

## 4. 下一步

ER-801：后台分页/筛选/排序下沉存储层；ER-810 统一质量数据源。
