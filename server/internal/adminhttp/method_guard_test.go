package adminhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
)

func TestEveryAdminRouteRejectsWrongMethod(t *testing.T) {
	guard := httpx.MethodGuard(routeSpecs, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, spec := range routeSpecs {
		wrong := http.MethodPatch
		if spec.Method == http.MethodPatch {
			wrong = http.MethodDelete
		}
		req := httptest.NewRequest(wrong, spec.Path, nil)
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status=%d, want 405", wrong, spec.Path, rec.Code)
		}
		var env httpx.Envelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s %s: 非 JSON 信封: %v", wrong, spec.Path, err)
		}
		if env.Code != int(errcode.SrvMethodNotAllowed) {
			t.Fatalf("%s %s: code=%d", wrong, spec.Path, env.Code)
		}
	}
}
