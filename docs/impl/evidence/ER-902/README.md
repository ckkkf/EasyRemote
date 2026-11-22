# ER-902 证据：安全矩阵
- 状态: 完成（cargo audit 已跑，零漏洞；仅 unmaintained 警告）
- 新测试：signal/ws_security_test.go（TestOriginAllowlist / TestWSUpgraderUsesAllowlist）
- 扫描：scripts/check-secrets.mjs → 505 files OK；web-admin npm audit --omit=dev → 0
- 既有负向：TestCrossDeviceAccessRejected、rotation/revocation、logout token、permission matrix
- 闸门：PASS（新增 secret-scan 步骤）
- 依赖修复：url 2.5.8→2.5.9；openh264 0.6.6→0.8.1（API 适配）；cargo audit 409 crates exit 0
- 未完成：日志脱敏自动扫描、真实浏览器 Origin E2E
- 原始：security-matrix.md、npm-audit.log、secret-scan.log、gate.log