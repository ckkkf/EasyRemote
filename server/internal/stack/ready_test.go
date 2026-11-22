package stack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/easyremote/server/internal/errcode"
)

// ER-903e：/readyz 必须反映依赖健康；/healthz 不受影响。
func TestReadyReflectsDependencyHealth(t *testing.T) {
	h := MountReady(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), ReadyCheck{Name: "postgres", Code: errcode.SrvDbUnavailable, Fn: func(context.Context) error {
		return errors.New("db down")
	}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusInternalServerError {
		t.Fatalf("ready status=%d, want 5xx", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status=%d, want 200", rec.Code)
	}

	ok := MountReady(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadyCheck{Name: "postgres", Fn: func(context.Context) error { return nil }})
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready ok status=%d", rec.Code)
	}
}
