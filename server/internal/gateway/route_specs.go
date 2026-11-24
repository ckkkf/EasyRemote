package gateway

import "github.com/easyremote/server/internal/httpx"

// routeSpecs 是代码对契约 method/auth 的显式声明；scripts/check-routes.mjs 会逐条核对，
// 运行时的方法闸门/鉴权在 ER-012/ER-013 使用同一份声明。
var routeSpecs = []httpx.RouteSpec{
	{ID: "SYS-01", Method: "GET", Path: "/healthz", Auth: "none"},
	{ID: "SYS-02", Method: "GET", Path: "/readyz", Auth: "none"},
	{ID: "GW-01", Method: "POST", Path: "/v1/auth/sms", Auth: "none"},
	{ID: "GW-02", Method: "POST", Path: "/v1/auth/login", Auth: "none"},
	{ID: "GW-03", Method: "GET", Path: "/v1/devices", Auth: "bearer"},
	{ID: "GW-04", Method: "POST", Path: "/v1/devices/bind", Auth: "bearer"},
	{ID: "GW-05", Method: "POST", Path: "/v1/devices/verify-code", Auth: "device"},
	{ID: "GW-06", Method: "POST", Path: "/v1/connect/request", Auth: "bearer"},
	{ID: "GW-07", Method: "POST", Path: "/v1/connect/consent", Auth: "device"},
	{ID: "GW-08", Method: "GET", Path: "/v1/connect/status", Auth: "bearer"},
	{ID: "GW-09", Method: "POST", Path: "/v1/devices/identity", Auth: "device"},
	{ID: "GW-10", Method: "GET", Path: "/v1/connect/keys", Auth: "bearer"},
	{ID: "GW-11", Method: "POST", Path: "/v1/relay/ticket", Auth: "device"},
	{ID: "GW-12", Method: "POST", Path: "/v1/auth/email/code", Auth: "none"},
	{ID: "GW-13", Method: "POST", Path: "/v1/auth/email/login", Auth: "none"},
	{ID: "GW-14", Method: "POST", Path: "/v1/files", Auth: "bearer"},
	{ID: "GW-15", Method: "GET", Path: "/v1/files/", Auth: "bearer"},
	{ID: "GW-16", Method: "POST", Path: "/v1/assist/create", Auth: "bearer"},
	{ID: "GW-17", Method: "POST", Path: "/v1/assist/redeem", Auth: "bearer"},
	{ID: "GW-18", Method: "GET", Path: "/v1/config", Auth: "none"},
	{ID: "GW-19", Method: "GET", Path: "/v1/net/reflect", Auth: "none"},
	{ID: "GW-20", Method: "POST", Path: "/v1/devices/password", Auth: "bearer"},
	{ID: "GW-25", Method: "POST", Path: "/v1/sessions/close", Auth: "device"},
	{ID: "GW-24", Method: "POST", Path: "/v1/sessions/ready", Auth: "device"},
	{ID: "GW-26", Method: "POST", Path: "/v1/sessions/quality", Auth: "device"},
	{ID: "GW-27", Method: "POST", Path: "/v1/sessions/health", Auth: "device"},
	{ID: "GW-21", Method: "GET", Path: "/v1/files", Auth: "bearer"},
	{ID: "GW-22", Method: "DELETE", Path: "/v1/files/", Auth: "bearer"},
	{ID: "GW-23", Method: "POST", Path: "/v1/files/", Auth: "bearer"},
}
