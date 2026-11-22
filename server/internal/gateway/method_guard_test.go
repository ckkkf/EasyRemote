package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
)

// 单元层：契约里的每条路由，未声明的 method 都必须被 MethodGuard 拒绝。
func TestEveryRouteRejectsWrongMethod(t *testing.T) {
	guard := httpx.MethodGuard(routeSpecs, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, spec := range routeSpecs {
		for _, method := range []string{http.MethodPatch, http.MethodTrace} {
			if method == spec.Method {
				continue
			}
			req := httptest.NewRequest(method, spec.Path, nil)
			rec := httptest.NewRecorder()
			guard.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status=%d, want 405", method, spec.Path, rec.Code)
			}
			var env httpx.Envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("%s %s: 非 JSON 错误信封: %v", method, spec.Path, err)
			}
			if env.Code != int(errcode.SrvMethodNotAllowed) {
				t.Fatalf("%s %s: code=%d, want %d", method, spec.Path, env.Code, errcode.SrvMethodNotAllowed)
			}
			if !strings.Contains(rec.Header().Get("Allow"), spec.Method) {
				t.Fatalf("%s %s: Allow=%q 缺 %s", method, spec.Path, rec.Header().Get("Allow"), spec.Method)
			}
		}
	}
}

// 集成层：真实 Routes() 已经被 MethodGuard 包住（不是只测中间件本身）。
func TestGatewayRoutesRejectWrongMethod(t *testing.T) {
	f := newFixture(t)
	req, err := http.NewRequest(http.MethodPatch, f.srv.URL+"/v1/auth/sms", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", res.StatusCode)
	}
	var env httpx.Envelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		t.Fatalf("非 JSON 信封: %v", err)
	}
	if env.Code != int(errcode.SrvMethodNotAllowed) {
		t.Fatalf("code=%d, want %d", env.Code, errcode.SrvMethodNotAllowed)
	}
}
