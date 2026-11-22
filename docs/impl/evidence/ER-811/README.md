# ER-811 证据：登出撤销与 CSP
- 状态: 完成
- 变更：api.logout；App 登出撤销；sessionStorage；securityHeaders；index CSP meta。
- 测试：TestLogoutRevokesTokenAndSecurityHeaders PASS；TestAdminIndexHasRestrictiveCsp PASS；gate PASS。
- 残余：静态托管 CSP 头、XSS 同页读取（ADR-0006）。
