# ADR-0006: 管理后台 token 威胁模型

- 状态: 接受
- 日期: 2026-11-22
- 关联: ER-811、AGENTS §4.10/§9.5

## 背景

管理后台是浏览器应用，token 无法放系统 Keychain。之前 token 存 localStorage，长期跨标签页/重启保留，XSS 可直接窃取。

## 决策

1. token 与身份信息改存 sessionStorage（标签页生命周期，关闭即清）。
2. 登出必须调用服务端 /admin/v1/logout 撤销会话；旧 token 立即失效（服务端测试覆盖）。
3. 服务端 API 响应带 Content-Security-Policy: default-src 'none'; frame-ancestors 'none'、X-Content-Type-Options: nosniff、Referrer-Policy: no-referrer。
4. 后台 UI 的 index.html 带 CSP meta：script-src 'self'、禁止外域脚本、frame-ancestors 'none'；开发 HMR 只放行 ws。
5. token 有效期由服务端 adminsession 控制，前端不自行延长。

## 残余风险

- XSS 仍可在同一标签页内读取 sessionStorage；CSP 与无内联脚本降低注入面，但不等于零。
- 生产静态托管必须同时下发 CSP 响应头（meta 不能覆盖所有场景）。
- 多标签页不共享登录（sessionStorage 隔离），属可接受 UX 代价。

## 验证

- TestLogoutRevokesTokenAndSecurityHeaders：登出后旧 token 失效 + CSP/nosniff。
- web-admin tsc/vitest 通过。
