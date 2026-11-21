package gateway

import (
	"net/http"
	"testing"
)

type clientConfigBody struct {
	Config struct {
		Session struct {
			MaxMinutes int64 `json:"max_minutes"`
		} `json:"session"`
		File struct {
			MaxBytes int64 `json:"max_bytes"`
		} `json:"file"`
		Diag struct {
			AutoRefreshSecs int64 `json:"auto_refresh_secs"`
		} `json:"diag"`
		Client struct {
			ForceUpdate bool `json:"force_update"`
		} `json:"client"`
	} `json:"config"`
}

func getClientConfig(t *testing.T, f *fixture) clientConfigBody {
	t.Helper()
	status, raw := f.do(http.MethodGet, "/v1/config", nil, map[string]string{
		"Authorization": "Bearer " + f.acct.Token,
	})
	if status != 200 {
		t.Fatalf("GET /v1/config: %d %s", status, raw)
	}
	var body clientConfigBody
	if err := unmarshalData(raw, &body); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return body
}

// TestClientConfigDefaults 校验白名单键的默认值（客户端无需实现默认逻辑）。
func TestClientConfigDefaults(t *testing.T) {
	f := newFixture(t)
	body := getClientConfig(t, f)
	if body.Config.Session.MaxMinutes != 720 {
		t.Fatalf("session.max_minutes = %d", body.Config.Session.MaxMinutes)
	}
	if body.Config.File.MaxBytes != 33554432 {
		t.Fatalf("file.max_bytes = %d", body.Config.File.MaxBytes)
	}
	if body.Config.Diag.AutoRefreshSecs != 30 {
		t.Fatalf("diag.auto_refresh_secs = %d", body.Config.Diag.AutoRefreshSecs)
	}
	if body.Config.Client.ForceUpdate {
		t.Fatal("client.force_update default must be false")
	}
}

// TestClientConfigFollowsStoredOverride 校验改过的键会真实下发。
func TestClientConfigFollowsStoredOverride(t *testing.T) {
	f := newFixture(t)
	if err := f.mem.PutConfig(t.Context(), "diag.auto_refresh_secs", "7"); err != nil {
		t.Fatal(err)
	}
	if err := f.mem.PutConfig(t.Context(), "client.force_update", "true"); err != nil {
		t.Fatal(err)
	}
	body := getClientConfig(t, f)
	if body.Config.Diag.AutoRefreshSecs != 7 {
		t.Fatalf("diag.auto_refresh_secs = %d", body.Config.Diag.AutoRefreshSecs)
	}
	if !body.Config.Client.ForceUpdate {
		t.Fatal("client.force_update override ignored")
	}
}

// TestClientConfigRequiresAuth 匿名不能读配置。
func TestClientConfigRequiresAuth(t *testing.T) {
	f := newFixture(t)
	status, raw := f.do(http.MethodGet, "/v1/config", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %s", status, raw)
	}
}

// TestNetReflectEchoesPeer 校验出口地址回显（P1-3 HTTP 侧）。
func TestNetReflectEchoesPeer(t *testing.T) {
	f := newFixture(t)
	status, raw := f.do(http.MethodGet, "/v1/net/reflect", nil, nil)
	if status != 200 {
		t.Fatalf("reflect: %d %s", status, raw)
	}
	var body struct {
		IP     string `json:"ip"`
		Port   int    `json:"port"`
		Source string `json:"source"`
	}
	if err := unmarshalData(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.IP != "127.0.0.1" && body.IP != "::1" {
		t.Fatalf("unexpected ip %q", body.IP)
	}
	if body.Port == 0 {
		t.Fatal("port should be the observed TCP source port")
	}
	if body.Source != "http-tcp" {
		t.Fatalf("source = %q, must not pretend to be STUN", body.Source)
	}
}
