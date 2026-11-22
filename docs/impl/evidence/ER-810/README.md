# ER-810 证据：后台身份与权限
- 状态: 完成（登出撤销归 ER-811）
- 变更：AdminSession.permissions；Permissions()；前端 identity/context/useCan；配置导航过滤；只读隐藏写操作。
- 测试：TestPermissionsReadonlyCannotWrite PASS；tsc PASS；gate PASS。
- 未完成：服务端 logout 调用、token 存储。
