# ER-904 最终验收审计（Release Readiness）

日期：2026-11-22。结论：**NO-GO**（不得宣称全部卡片完成）。

## 闸门

    $ bash scripts/gate.sh
      scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
    GATE: PASS

闸门 20 步（新增 rust-ffi-cdylib、secret-scan）。

## 卡片统计

- 任务卡总数：65
- 完成：20
- 部分/未完成：45（其中多张已显式 disabled 或标注未验证）

## 阻止发布的事项

| # | 事项 | 影响 | 证据 |
|---|---|---|---|
| 1 | Windows 桌面矩阵未运行 | 无法证明 Windows 采集/注入/打包 | ER-900 matrix |
| 2 | Xcode 未安装 | macOS 纹理/打包未编译验证 | ER-601b/ER-900 |
| 3 | 文件/剪贴板未接会话 data stream | 能力 disabled，功能不可用 | ADR-0005 / ER-710/711 |
| 4 | ~~30min soak/RSS 未跑~~（已补：macOS Software 1800s，RSS -16.65%） | Windows/硬件编码 soak 仍缺 | ER-901a |
| 5 | ~~cargo audit 未安装~~（已补：url/openh264 升级，audit exit 0；ttf-parser unmaintained 警告） | 无已知漏洞；unmaintained 需跟踪 | ER-902 |
| 6 | Redis/PG/Relay 进程重启、磁盘满未注入 | Chaos 不完整 | ER-903 |
| 7 | 后台设备/会话/审计前端仍部分客户端分页 | 分页/总数不完全可信 | ER-801a |
| 8 | 隐私屏/防睡眠能力未启用 | 产品能力缺失 | ADR-0004/ER-702 |

## 已完成且可复验的核心闭环

- 统一闸门、契约/错误/路由三方对齐、MethodGuard。
- 会话状态机 CAS/ready/关闭传播/幂等/过期/唯一在线对。
- 信令 Bus/presence/TTL/Origin/64KB/认证 deadline。
- P2P 打洞/Relay 回落/可靠注册/弱网 E2E。
- 系统安全存储、CSPRNG、FFI 字符串释放。
- 后台身份/权限/登出撤销/CSP/中断 partial 结果。
- active 僵尸 janitor（30s 窗口）。

## 验收原则

- 本审计不把 partial 写成完成；所有未运行项按 AGENTS §11.2 计未完成。
- 发布前必须逐项关闭上表，或在任务卡中由负责人明确接受降级并更新能力台账/文案。