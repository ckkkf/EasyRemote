package signal

import (
	"net/http"
	"testing"
)

// ER-902：WebSocket Origin 必须默认拒绝非本机；配置后只精确放行。
func TestOriginAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		origin  string
		allowed []string
		want    bool
	}{
		{"empty origin native", "", nil, true},
		{"localhost default", "http://localhost:5173", nil, true},
		{"loopback default", "http://127.0.0.1:8080", nil, true},
		{"evil default rejected", "https://evil.example", nil, false},
		{"configured exact", "https://admin.example", []string{"https://admin.example"}, true},
		{"configured mismatch", "https://evil.example", []string{"https://admin.example"}, false},
		{"subdomain not exact", "https://a.admin.example", []string{"https://admin.example"}, false},
	}
	for _, c := range cases {
		if got := OriginAllowed(c.origin, c.allowed); got != c.want {
			t.Fatalf("%s: OriginAllowed(%q)=%v want %v", c.name, c.origin, got, c.want)
		}
	}
}

func TestWSUpgraderUsesAllowlist(t *testing.T) {
	up := WSUpgrader([]string{"https://admin.example"})
	req, _ := http.NewRequest(http.MethodGet, "/v1/signal/ws", nil)
	req.Header.Set("Origin", "https://evil.example")
	if up.CheckOrigin(req) {
		t.Fatal("evil origin must be rejected by WSUpgrader")
	}
	req.Header.Set("Origin", "https://admin.example")
	if !up.CheckOrigin(req) {
		t.Fatal("allowlisted origin must be accepted")
	}
}
