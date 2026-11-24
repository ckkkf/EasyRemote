package session

import (
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
)

// ER-903g：时钟回退不能让人工授权/就绪被误判过期，也不能产生 panic。
func TestClockBackwardDoesNotExpireConsent(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, err := s.Request(ctx(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	// 本机时钟回退 1 小时。
	s.SetClock(func() time.Time { return now.Add(-time.Hour) })
	got, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatalf("backward clock must not time out consent: %v", err)
	}
	if got.State != StateAuthorized {
		t.Fatalf("state=%s", got.State)
	}
	if _, err := s.Ready(ctx(), sess.ID, "t1"); err != nil {
		t.Fatalf("backward clock must not expire auth window: %v", err)
	}
}

// 时钟前跳超过 ConsentTimeout：requested 必须过期/拒绝，不能永久等待。
func TestClockForwardExpiresConsent(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, err := s.Request(ctx(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return now.Add(ConsentTimeout + time.Second) })
	got, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatalf("want nil error (complete state), got %v", err)
	}
	if got.State != StateClosed || got.Reason != "consent_timeout" {
		t.Fatalf("want closed/consent_timeout, got %s/%q", got.State, got.Reason)
	}
}

// 时钟前跳超过 AuthWindow：ready 必须失败。
func TestClockForwardExpiresAuthWindow(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c1", "t1")
	if _, err := s.Consent(ctx(), sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return now.Add(AuthWindow + time.Second) })
	if _, err := s.Ready(ctx(), sess.ID, "t1"); !errcode.Is(err, errcode.SessionAuthWindowExceeded) {
		t.Fatalf("want AUTH_WINDOW_EXCEEDED, got %v", err)
	}
}
