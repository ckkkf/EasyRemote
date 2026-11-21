package session

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

func setup(t *testing.T) (*Service, *store.Memory) {
	t.Helper()
	mem := store.NewMemory()
	return New(mem), mem
}

func TestRequestIsIdempotent(t *testing.T) {
	s, _ := setup(t)
	a, err := s.Request(ctx(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Request(ctx(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("want same session, got %s and %s", a.ID, b.ID)
	}
}

func TestSelfConnectRejected(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.Request(ctx(), "d1", "d1"); !errcode.Is(err, errcode.ConnSelfConnect) {
		t.Fatalf("want CONN_SELF_CONNECT, got %v", err)
	}
}

func TestConsentAcceptedOpensAuthWindow(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	got, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateAuthorized {
		t.Fatalf("want authorized, got %s", got.State)
	}
	if time.Until(got.AuthExpireAt) < 11*time.Hour {
		t.Fatalf("auth window too short: %v", got.AuthExpireAt)
	}
}

func TestConsentRejected(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	_, err := s.Consent(ctx(), sess.ID, false, "user_rejected")
	if !errcode.Is(err, errcode.ConnConsentRejected) {
		t.Fatalf("want rejected, got %v", err)
	}
}

func TestConsentTimeout(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.SetClock(func() time.Time { return now.Add(31 * time.Second) })
	if _, err := s.Consent(ctx(), sess.ID, true, ""); !errcode.Is(err, errcode.ConnConsentTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestActivateAfterAuthWindowFails(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.Consent(ctx(), sess.ID, true, "")
	s.SetClock(func() time.Time { return now.Add(13 * time.Hour) })
	if _, err := s.Activate(ctx(), sess.ID); !errcode.Is(err, errcode.SessionAuthWindowExceeded) {
		t.Fatalf("want auth window exceeded, got %v", err)
	}
}

func TestKillByAdmin(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.Consent(ctx(), sess.ID, true, "")
	closed, err := s.Kill(ctx(), sess.ID, "admin: abuse")
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != StateClosed || closed.Reason != "admin: abuse" {
		t.Fatalf("unexpected: %+v", closed)
	}
	// 已关闭会话再次关闭：按「不存在可关闭的会话」处理（而不是语义错误的 SessionExpired）
	if _, err := s.Kill(ctx(), sess.ID, "again"); !errcode.Is(err, errcode.SessionNotFound) {
		t.Fatalf("want SESSION_NOT_FOUND for closed session, got %v", err)
	}
}

func TestGetMissingSession(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.Get(ctx(), "nope"); !errcode.Is(err, errcode.SessionNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}
func TestActivateWithoutConsentRejected(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	if _, err := s.Activate(ctx(), sess.ID); err == nil {
		t.Fatal("activate without consent must fail")
	}
}

func TestActivateTwiceIsRejectedOrIdempotentSafe(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.Consent(ctx(), sess.ID, true, "")
	if _, err := s.Activate(ctx(), sess.ID); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	// 第二次激活：active → active 不在白名单，必须失败
	if _, err := s.Activate(ctx(), sess.ID); err == nil {
		t.Fatal("second activate must fail")
	}
}

func TestConsentOnClosedSession(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.Kill(ctx(), sess.ID, "done")
	if _, err := s.Consent(ctx(), sess.ID, true, ""); err == nil {
		t.Fatal("consent on closed session must fail")
	}
}

func ctx() context.Context { return context.Background() }
