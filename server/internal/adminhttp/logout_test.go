package adminhttp

import (
	"net/http"
	"testing"
)

// ER-811：登出必须撤销服务端会话；旧 token 立即失效，且响应带安全头。
func TestLogoutRevokesTokenAndSecurityHeaders(t *testing.T) {
	f := newAdminFixture(t)
	code, raw := f.do(http.MethodGet, "/admin/v1/stats/overview", nil)
	if code != http.StatusOK {
		t.Fatalf("overview before logout=%d %s", code, raw)
	}
	code, raw = f.do(http.MethodPost, "/admin/v1/logout", nil)
	if code != http.StatusOK {
		t.Fatalf("logout=%d %s", code, raw)
	}
	code, raw = f.do(http.MethodGet, "/admin/v1/stats/overview", nil)
	if code == http.StatusOK {
		t.Fatalf("old token still valid after logout: %s", raw)
	}
	// 安全头：至少 CSP/nosniff 必须存在。
	res, err := http.Get(f.srv.URL + "/admin/v1/login")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy")
	}
	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}
