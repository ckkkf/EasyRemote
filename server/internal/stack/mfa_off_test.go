package stack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/store"
)

// 生产环境配 ER_ADMIN_TOTP_OFF 必须直接失败：安全要求不能被环境变量静默关掉。
func TestProductionRefusesMFADisabled(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_ADMIN_TOTP_OFF", "1")
	_, err := NewWithStoreAndSessions(store.NewMemory(), adminsession.New(nil, time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	if err == nil || !strings.Contains(err.Error(), "ER_ADMIN_TOTP_OFF") {
		t.Fatalf("want production refusal, got %v", err)
	}
}

// 开发环境打开开关后，空二次验证码也能登录后台。
func TestDevMFADisabledAllowsEmptyCode(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_ADMIN_TOTP_OFF", "1")
	mem := store.NewMemory()
	if err := SeedDevAdmin(context.Background(), mem); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h, err := NewWithStoreAndSessions(mem, adminsession.New(nil, time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	req := httptest.NewRequest("POST", "/admin/v1/login", strings.NewReader(`{"username":"root","password":"dev-pass","totp":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"username":"root"`) {
		t.Fatalf("unexpected body %s", w.Body.String())
	}
}

// 开关关闭时（默认），空二次验证码必须被拒。
func TestDevMFAOnRejectsEmptyCode(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_ADMIN_TOTP_OFF", "")
	mem := store.NewMemory()
	if err := SeedDevAdmin(context.Background(), mem); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h, err := NewWithStoreAndSessions(mem, adminsession.New(nil, time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	req := httptest.NewRequest("POST", "/admin/v1/login", strings.NewReader(`{"username":"root","password":"dev-pass","totp":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %s", w.Code, w.Body.String())
	}
}
