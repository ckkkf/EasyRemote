package httpx

// RouteSpec 是代码侧对契约端点（method/path/auth）的显式声明。
//
// 为什么单独声明：路由注册本身只带 path，checker 无法从 handler 体里可靠推断
// 鉴权方式。把声明集中在这里，scripts/check-routes.mjs 就能把「契约 ↔ 声明 ↔
// 已注册路径」三方对齐；运行时的方法闸门/鉴权（ER-012/ER-013）复用同一份声明。
type RouteSpec struct {
	ID     string
	Method string
	Path   string
	Auth   string
}
