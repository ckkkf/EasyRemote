package errcode

import (
	"net/http"
	"testing"
)

func TestGivenCodesHaveMeta(t *testing.T) {
	// 核心码必须有明确的用户文案与状态，不能落到默认值
	for _, c := range []Code{AuthTokenExpired, ConnCaptchaLocked, SessionAuthWindowExceeded, AdminForbidden, SrvInternal} {
		m := Describe(c)
		if m.Message == defaultMeta.Message && c != SrvInternal {
			t.Fatalf("code %d falls back to default meta", c)
		}
		if m.Status == 0 {
			t.Fatalf("code %d has zero status", c)
		}
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[Code]int{
		AuthTokenExpired:  http.StatusUnauthorized,
		AdminForbidden:    http.StatusForbidden,
		ConnCaptchaLocked: http.StatusTooManyRequests,
		SrvInternal:       http.StatusInternalServerError,
		SessionNotFound:   http.StatusNotFound,
	}
	for c, want := range cases {
		if got := Describe(c).Status; got != want {
			t.Fatalf("code %d status=%d want %d", c, got, want)
		}
	}
}

func TestRetryableFlags(t *testing.T) {
	if !Describe(ConnRelayUnavailable).Retryable {
		t.Fatal("relay unavailable should be retryable")
	}
	if Describe(AdminForbidden).Retryable {
		t.Fatal("forbidden must not be retryable")
	}
}
