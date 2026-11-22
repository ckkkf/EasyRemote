# ER-900 桌面关键路径矩阵

日期：2026-11-22
环境：macOS（本机），Windows 未接入本会话。

## 矩阵

| 能力 | macOS | Windows | 证据/说明 |
|---|---|---|---|
| 屏幕采集 | 待真机确认 | 未运行 | er-capture 平台测试；macOS 需屏幕录制权限 |
| 输入注入 | 待真机确认 | 未运行 | er-input 平台测试；macOS 需辅助功能权限 |
| 原生纹理 | **未完成** | 未实现 | Xcode 未安装，FlutterTexture 未编译；RGBA 回退可用 |
| 登录/绑定 | 通过 | 未运行 | client/test/host_link_test.dart（E2eServices 自动起服务） |
| 申请/授权 | 通过 | 未运行 | session_e2e_test.dart + consent 测试 |
| P2P/Relay 选择 | 通过（relay/打洞测试） | 未运行 | p2p/relay_client 测试 + lossy_e2e |
| 弱网 | 通过 | 未运行 | lossy_e2e_test.dart（5% 丢包/抖动/重排） |
| 断开传播 | 通过 | 未运行 | session/dispatcher/admin close 测试 |
| 打包 | **未完成** | **未完成** | flutter build macos 因 Xcode 缺失；Windows 未构建 |

## 无 skip 校验

- client/test/host_link_test.dart、session_e2e_test.dart 注释与实现：缺服务时失败，不 skip。
- native_test.dart 在无 FFI/显示环境会打印 skip；这些属于本机能力探测，不是跨进程 E2E。
- 本仓库未发现 zz_dbg_test.dart 等调试测试参与正式 flutter test。

## 执行命令

    bash scripts/desktop/macos_smoke.sh
    # Windows: powershell -File scripts/desktop/windows_smoke.ps1

## 结论

- macOS 跨进程 E2E 与核心平台测试可在本机运行；纹理/打包受 Xcode 缺失阻塞。
- Windows 全矩阵未执行，不能宣称 ER-900 完成。
