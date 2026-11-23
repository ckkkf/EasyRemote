package stack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/ratelimit"
)

// 开发环境打开 ER_RATELIMIT_OFF 后必须拿到无限流器，否则本地反复登录仍会被锁。
func TestDevRateLimitOffReturnsUnlimited(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_RATELIMIT_OFF", "1")
	lim, err := OpenLimiter(context.Background())
	if err != nil {
		t.Fatalf("open limiter: %v", err)
	}
	if _, ok := lim.(*ratelimit.Unlimited); !ok {
		t.Fatalf("want *ratelimit.Unlimited, got %T", lim)
	}
	// 网关登录阈值是 10 次/小时：连打 30 次仍必须全部放行。
	for i := 0; i < 30; i++ {
		res, err := lim.Allow(context.Background(), "login:phone:13800000000", 10, time.Hour)
		if err != nil {
			t.Fatalf("allow %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("attempt %d blocked under ER_RATELIMIT_OFF", i)
		}
	}
}

// 生产环境配 ER_RATELIMIT_OFF 必须直接失败：限流不能被环境变量静默关掉。
func TestProductionRefusesRateLimitOff(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_RATELIMIT_OFF", "1")
	_, err := OpenLimiter(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ER_RATELIMIT_OFF") {
		t.Fatalf("want production refusal, got %v", err)
	}
}

// 开关未设置时，开发环境仍必须走真实的内存限流器（默认严格）。
func TestDevWithoutFlagUsesMemoryLimiter(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_RATELIMIT_OFF", "")
	lim, err := OpenLimiter(context.Background())
	if err != nil {
		t.Fatalf("open limiter: %v", err)
	}
	if _, ok := lim.(*ratelimit.Memory); !ok {
		t.Fatalf("want *ratelimit.Memory by default, got %T", lim)
	}
}

// devLimiter 是装配层入口：NewDev/NewPostgres 都靠它，别再直接写死 NewMemory。
func TestDevLimiterHonoursFlag(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_RATELIMIT_OFF", "1")
	if _, ok := devLimiter().(*ratelimit.Unlimited); !ok {
		t.Fatalf("devLimiter must be Unlimited when flag on")
	}
	t.Setenv("ER_RATELIMIT_OFF", "")
	if _, ok := devLimiter().(*ratelimit.Memory); !ok {
		t.Fatalf("devLimiter must be Memory by default")
	}
}

// 端到端：开关打开后，一体化 stack 的 /v1/auth/login 连打 20 次不得出现 100004。
func TestDevStackLoginNotLimitedWhenOff(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_RATELIMIT_OFF", "1")
	_, h, err := NewDev()
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for i := 0; i < 20; i++ {
		code := postLogin(t, h, "13800001111")
		if code != 0 {
			t.Fatalf("attempt %d rejected with code %d under ER_RATELIMIT_OFF", i+1, code)
		}
	}
}

// 端到端：开关关闭时默认仍然严格——第 11 次登录必须被限流（阈值 10 次/小时）。
func TestDevStackLoginLimitedByDefault(t *testing.T) {
	t.Setenv("ER_ENV", "")
	t.Setenv("ER_RATELIMIT_OFF", "")
	_, h, err := NewDev()
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var lastCode int
	for i := 0; i < 11; i++ {
		lastCode = postLogin(t, h, "13800002222")
	}
	if lastCode == 0 {
		t.Fatal("11th login must be rate limited when ER_RATELIMIT_OFF is unset")
	}
}

func postLogin(t *testing.T, h http.Handler, phone string) int {
	t.Helper()
	body := strings.NewReader(`{"phone":"` + phone + `","code":"123456"}`)
	req := httptest.NewRequest("POST", "/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var env struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return env.Code
}
