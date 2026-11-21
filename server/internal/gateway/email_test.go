package gateway

import (
	"net/http"
	"testing"

	"github.com/easyremote/server/internal/store"
)

// TestEmailLoginCreatesThenReusesAccount 覆盖 P0-4 主路径。
func TestEmailLoginCreatesThenReusesAccount(t *testing.T) {
	f := newFixture(t)

	status, raw := f.postJSONRaw("/v1/auth/email/code", map[string]any{"email": "Ralph@Example.com"}, nil)
	if status != 200 {
		t.Fatalf("email code: %d %s", status, raw)
	}
	var codeRes struct {
		OK    bool   `json:"ok"`
		Email string `json:"email"`
	}
	_ = unmarshalData(raw, &codeRes)
	if !codeRes.OK || codeRes.Email != "ralph@example.com" {
		t.Fatalf("code response must normalize email: %s", raw)
	}

	var first struct {
		Token     string `json:"token"`
		AccountID string `json:"account_id"`
	}
	status, raw = f.postJSONRaw("/v1/auth/email/login", map[string]any{
		"email": "ralph@example.com", "code": "654321",
	}, nil)
	if status != 200 {
		t.Fatalf("email login: %d %s", status, raw)
	}
	_ = unmarshalData(raw, &first)
	if first.Token == "" || first.AccountID == "" {
		t.Fatalf("missing token/account: %s", raw)
	}

	// 第二次登录（大小写不同）必须落到同一账号。
	var second struct {
		AccountID string `json:"account_id"`
	}
	status, raw = f.postJSONRaw("/v1/auth/email/login", map[string]any{
		"email": "RALPH@example.com", "code": "654321",
	}, nil)
	if status != 200 {
		t.Fatalf("second login: %d %s", status, raw)
	}
	_ = unmarshalData(raw, &second)
	if second.AccountID != first.AccountID {
		t.Fatalf("email should reuse account: %s vs %s", first.AccountID, second.AccountID)
	}

	// 令牌与手机号登录同构：能直接用于设备相关接口。
	var devices struct {
		Devices []map[string]any `json:"devices"`
	}
	status, raw = f.do(http.MethodGet, "/v1/devices", nil, map[string]string{
		"Authorization": "Bearer " + first.Token,
	})
	if status != 200 {
		t.Fatalf("token from email login must be usable: %d %s", status, raw)
	}
	_ = unmarshalData(raw, &devices)
}

// TestEmailLoginWrongCodeRejected 覆盖「错误码拒绝」。
func TestEmailLoginWrongCodeRejected(t *testing.T) {
	f := newFixture(t)
	status, raw := f.postJSONRaw("/v1/auth/email/login", map[string]any{
		"email": "nobody@example.com", "code": "123456",
	}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %s", status, raw)
	}
	if name := errName(raw); name != "AUTH_INVALID_CREDENTIAL" {
		t.Fatalf("want AUTH_INVALID_CREDENTIAL, got %s", name)
	}
	// 没通过校验就不该建号。
	accounts, _ := f.mem.ListAccounts(t.Context(), store.Page{Limit: 100})
	for _, a := range accounts {
		if a.PhoneEnc == "nobody@example.com" {
			t.Fatal("failed login must not create account")
		}
	}
}

// TestEmailCodeRateLimited 覆盖「限流触发」（60 秒一条）。
func TestEmailCodeRateLimited(t *testing.T) {
	f := newFixture(t)
	status, raw := f.postJSONRaw("/v1/auth/email/code", map[string]any{"email": "rate@example.com"}, nil)
	if status != 200 {
		t.Fatalf("first code: %d %s", status, raw)
	}
	status, raw = f.postJSONRaw("/v1/auth/email/code", map[string]any{"email": "rate@example.com"}, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d %s", status, raw)
	}
	if name := errName(raw); name != "AUTH_SMS_RATE_LIMITED" {
		t.Fatalf("want AUTH_SMS_RATE_LIMITED, got %s", name)
	}
	// 大小写不同也属于同一个 key。
	status, _ = f.postJSONRaw("/v1/auth/email/code", map[string]any{"email": "RATE@example.com"}, nil)
	if status != http.StatusTooManyRequests {
		t.Fatalf("case-insensitive rate limit: got %d", status)
	}
}
