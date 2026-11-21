package adminhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// adminFixture 起一套真后台路由（真登录、真会话、真审计）。
type adminFixture struct {
	t      *testing.T
	srv    *httptest.Server
	mem    *store.Memory
	admins *adminsession.Service
	token  string
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	mem := store.NewMemory()
	if err := mem.PutAdmin(t.Context(), &store.AdminUser{
		Username:     "root",
		PasswordHash: adminapi.HashPassword("dev-pass"),
		TOTPSecret:   "DEVTOTPSECRET",
		Role:         adminapi.RoleSuper,
		Status:       "active",
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	sessions := adminsession.New(nil, time.Hour)
	app := &App{
		Limiter:  ratelimit.NewMemory(),
		Service:  adminapi.New(mem, session.New(mem)).SetConfig(appconfig.New(mem)),
		Sessions: sessions,
	}
	srv := httptest.NewServer(app.Routes())
	t.Cleanup(srv.Close)
	f := &adminFixture{t: t, srv: srv, mem: mem, admins: sessions}

	body, _ := json.Marshal(map[string]string{
		"username": "root",
		"password": "dev-pass",
		"totp":     adminapi.TOTPCode("DEVTOTPSECRET", time.Now()),
	})
	res, err := http.Post(srv.URL+"/admin/v1/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("login failed: %d %s", res.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Data.Token == "" {
		t.Fatalf("no token: %s", raw)
	}
	f.token = env.Data.Token
	return f
}

func (f *adminFixture) do(method, path string, payload any) (int, []byte) {
	f.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			f.t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, f.srv.URL+path, body)
	if err != nil {
		f.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	res, err := f.srv.Client().Do(req)
	if err != nil {
		f.t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (f *adminFixture) getConfig() map[string]string {
	f.t.Helper()
	status, raw := f.do(http.MethodGet, "/admin/v1/config", nil)
	if status != 200 {
		f.t.Fatalf("GET config: %d %s", status, raw)
	}
	var env struct {
		Data struct {
			Config map[string]string `json:"config"`
			Fields []appconfig.Field `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		f.t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Fields) != 4 {
		f.t.Fatalf("want 4 fields, got %d", len(env.Data.Fields))
	}
	return env.Data.Config
}

// TestConfigReadDefaults 校验读接口下发默认值与表单 schema。
func TestConfigReadDefaults(t *testing.T) {
	f := newAdminFixture(t)
	cfg := f.getConfig()
	if cfg[appconfig.KeyFileMaxBytes] != "33554432" {
		t.Fatalf("file.max_bytes = %q", cfg[appconfig.KeyFileMaxBytes])
	}
	if cfg[appconfig.KeySessionMaxMinutes] != "720" {
		t.Fatalf("session.max_minutes = %q", cfg[appconfig.KeySessionMaxMinutes])
	}
}

// TestConfigWriteAudited 校验写接口落审计（before/after）。
func TestConfigWriteAudited(t *testing.T) {
	f := newAdminFixture(t)
	status, raw := f.do(http.MethodPut, "/admin/v1/config", map[string]any{
		"values": map[string]string{appconfig.KeyFileMaxBytes: "2097152"},
	})
	if status != 200 {
		t.Fatalf("PUT config: %d %s", status, raw)
	}
	if got := f.getConfig()[appconfig.KeyFileMaxBytes]; got != "2097152" {
		t.Fatalf("override not persisted: %q", got)
	}
	logs, err := f.mem.ListAudit(t.Context(), 50)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.AuditLog
	for _, l := range logs {
		if l.Action == "config.update" {
			found = l
			break
		}
	}
	if found == nil {
		t.Fatalf("missing config.update audit: %+v", logs)
	}
	if found.Before == "" || found.After == "" {
		t.Fatalf("audit must carry before/after: %+v", found)
	}
	if !bytes.Contains([]byte(found.Before), []byte("33554432")) ||
		!bytes.Contains([]byte(found.After), []byte("2097152")) {
		t.Fatalf("audit diff wrong: before=%s after=%s", found.Before, found.After)
	}
	if found.Result != "ok" {
		t.Fatalf("result = %q", found.Result)
	}
}

// TestConfigRejectsInvalidValue 校验取值域：越界/非数字一律拒绝且不落库。
func TestConfigRejectsInvalidValue(t *testing.T) {
	f := newAdminFixture(t)
	cases := []map[string]string{
		{appconfig.KeyFileMaxBytes: "abc"},
		{appconfig.KeyFileMaxBytes: "0"},
		{appconfig.KeySessionMaxMinutes: "99999"},
		{appconfig.KeyClientForceUpdate: "yes"},
		{"unknown.key": "1"},
	}
	for _, c := range cases {
		status, raw := f.do(http.MethodPut, "/admin/v1/config", map[string]any{"values": c})
		if status != http.StatusBadRequest {
			t.Fatalf("case %v: want 400, got %d %s", c, status, raw)
		}
		if name := envelopeName(raw); name != "ADMIN_CONFIG_INVALID" {
			t.Fatalf("case %v: want ADMIN_CONFIG_INVALID, got %s", c, name)
		}
	}
	// 非法写入不得影响已存值。
	if got := f.getConfig()[appconfig.KeyFileMaxBytes]; got != "33554432" {
		t.Fatalf("invalid writes must not persist, got %q", got)
	}
}

// TestConfigForbiddenForOps 校验只有超级管理员能读写配置。
func TestConfigForbiddenForOps(t *testing.T) {
	f := newAdminFixture(t)
	tok, _, err := f.admins.Issue(t.Context(), "adm-ops", "ops01", adminapi.RoleOps)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/admin/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 for ops, got %d", res.StatusCode)
	}
}

func envelopeName(raw []byte) string {
	var env struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &env)
	return env.Name
}
