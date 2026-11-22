# ER-902 安全测试矩阵

日期：2026-11-22。每条红线至少一个负向测试。

| 红线 | 负向测试/证据 | 结果 |
|---|---|---|
| allow_control 默认 false | host_policy_test.dart / prefs_test | PASS |
| 跨账号访问会话/公钥/同意/票据/文件 | TestCrossDeviceAccessRejected | PASS |
| 旧票据重放 | relay/rotation_test.go（epoch 递增后旧票失效） | PASS |
| 会话撤销 | relay/revocation_test.go + signal bus 测试 | PASS |
| WS Origin 非本机 | TestOriginAllowlist / TestWSUpgraderUsesAllowlist | PASS |
| 后台越权/只读写 | TestPermissionMatrix/PermissionsReadonlyCannotWrite | PASS |
| 登出旧 token | TestLogoutRevokesTokenAndSecurityHeaders | PASS |
| 隐私屏不得假装 | ADR-0004 + capability disabled | PASS |
| secret 泄漏 | scripts/check-secrets.mjs（505 files） | PASS |
| Web 依赖漏洞 | npm audit --omit=dev | 0 vulnerabilities |
| Rust 依赖漏洞 | cargo audit（409 crates） | PASS：url 2.5.8→2.5.9、openh264 0.6.6→0.8.1；仅 ttf-parser unmaintained 警告（无修复） |

## 命令

    cd server && go test ./internal/signal/ ./internal/gateway/ ./internal/relay/... ./internal/relaymgr/...
    node scripts/check-secrets.mjs
    cd web-admin && npm audit --omit=dev
    cd core && cargo audit   # 已安装 0.23.0

## 未完成

- cargo audit（RustSec）未安装/未跑。
- 真实 Origin 浏览器预检/跨站 WebSocket 攻击的端到端测试。
- 日志脱敏无自动扫描（仅代码审查）。
- Windows/生产 TLS 未在本机验证。