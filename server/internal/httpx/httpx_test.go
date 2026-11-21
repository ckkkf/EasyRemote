package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/easyremote/server/internal/errcode"
)

func TestErrHidesDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	Err(rec, req, errcode.New(errcode.AdminForbidden, "internal: account table missing"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "account table missing") {
		t.Fatal("internal detail leaked to caller")
	}
	if !strings.Contains(body, "trace_id") {
		t.Fatal("missing trace_id")
	}
	if !strings.Contains(body, "ADMIN_FORBIDDEN") {
		t.Fatal("missing error name")
	}
}

func TestErrUnhandledBecomesInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	Err(rec, httptest.NewRequest("GET", "/x", nil), errors.New("db exploded: password=hunter2"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatal("secret leaked")
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1,"evil":2}`))
	var v struct {
		A int `json:"a"`
	}
	if err := Decode(rec, req, &v); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestMiddlewareRecoversPanic(t *testing.T) {
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Trace-Id") == "" {
		t.Fatal("missing trace header")
	}
}

func TestTraceIDIsUnique(t *testing.T) {
	a, b := TraceID(), TraceID()
	if a == b || len(a) < 8 {
		t.Fatalf("bad trace ids: %q %q", a, b)
	}
}
