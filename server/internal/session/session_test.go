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

// ER-905：拒绝是完成态决策（契约 GW-07 的 ConsentResult），不再返回业务错误。
func TestConsentRejected(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c1", "t1")
	got, err := s.Consent(ctx(), sess.ID, false, "user_rejected")
	if err != nil {
		t.Fatalf("want nil error (complete state), got %v", err)
	}
	if got.State != StateClosed || got.Reason != "user_rejected" {
		t.Fatalf("want closed/user_rejected, got %s/%q", got.State, got.Reason)
	}
}

func TestConsentTimeout(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.SetClock(func() time.Time { return now.Add(31 * time.Second) })
	got, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatalf("want nil error (complete state), got %v", err)
	}
	if got.State != StateClosed || got.Reason != "consent_timeout" {
		t.Fatalf("want closed/consent_timeout, got %s/%q", got.State, got.Reason)
	}
}

func TestReadyAfterAuthWindowFails(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c1", "t1")
	s.Consent(ctx(), sess.ID, true, "")
	_, _ = s.Ready(ctx(), sess.ID, "c1")
	s.SetClock(func() time.Time { return now.Add(13 * time.Hour) })
	if _, err := s.Ready(ctx(), sess.ID, "t1"); !errcode.Is(err, errcode.SessionAuthWindowExceeded) {
		t.Fatalf("want auth window exceeded, got %v", err)
	}
	got, _ := s.Get(ctx(), sess.ID)
	if got.State != StateClosed || got.Reason != "auth_window_exceeded" {
		t.Fatalf("expired window must close session: %+v", got)
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
	// 重复关闭是幂等事件：返回已关闭会话，不重复写 state/reason。
	again, err := s.Kill(ctx(), sess.ID, "again")
	if err != nil {
		t.Fatalf("duplicate kill should be idempotent: %v", err)
	}
	if again.State != StateClosed || again.Reason != "admin: abuse" {
		t.Fatalf("duplicate kill changed terminal state: %+v", again)
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
	_, _ = s.Ready(ctx(), sess.ID, "c1")
	_, _ = s.Ready(ctx(), sess.ID, "t1")
	if _, err := s.Activate(ctx(), sess.ID); err != nil {
		t.Fatalf("first activate: %v", err)
	}
	// 第二次激活：active → active 是重复事件，必须幂等成功且不改状态。
	second, err := s.Activate(ctx(), sess.ID)
	if err != nil {
		t.Fatalf("duplicate activate should be idempotent: %v", err)
	}
	if second.State != StateActive {
		t.Fatalf("state=%s, want active", second.State)
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
