package adminhttp

import "github.com/easyremote/server/internal/httpx"

// routeSpecs 是代码对契约 method/auth 的显式声明；scripts/check-routes.mjs 会逐条核对，
// 运行时的方法闸门/鉴权在 ER-012/ER-013 使用同一份声明。
var routeSpecs = []httpx.RouteSpec{
	{ID: "ADM-01", Method: "POST", Path: "/admin/v1/login", Auth: "none"},
	{ID: "ADM-02", Method: "POST", Path: "/admin/v1/logout", Auth: "admin"},
	{ID: "ADM-03", Method: "GET", Path: "/admin/v1/stats/overview", Auth: "admin"},
	{ID: "ADM-04", Method: "GET", Path: "/admin/v1/users", Auth: "admin"},
	{ID: "ADM-05", Method: "POST", Path: "/admin/v1/users/ban", Auth: "admin"},
	{ID: "ADM-06", Method: "GET", Path: "/admin/v1/devices", Auth: "admin"},
	{ID: "ADM-07", Method: "GET", Path: "/admin/v1/sessions", Auth: "admin"},
	{ID: "ADM-08", Method: "POST", Path: "/admin/v1/sessions/kill", Auth: "admin"},
	{ID: "ADM-09", Method: "GET", Path: "/admin/v1/config", Auth: "admin"},
	{ID: "ADM-10", Method: "PUT", Path: "/admin/v1/config", Auth: "admin"},
	{ID: "ADM-11", Method: "GET", Path: "/admin/v1/audit", Auth: "admin"},
}
