package adminsession

import (
	"context"
	"testing"
	"time"
)

func bg() context.Context { return context.Background() }

func TestIssueAndVerify(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	tok, sess, err := s.Issue(bg(), "adm1", "root", "super")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" || sess == nil {
		t.Fatal("issue failed")
	}
	got, ok := s.Verify(bg(), tok)
	if !ok {
		t.Fatal("verify failed")
	}
	if got.Role != "super" || got.Username != "root" {
		t.Fatalf("unexpected session: %+v", got)
	}
}

func TestForgedTokenRejected(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	_, _, _ = s.Issue(bg(), "adm1", "root", "super")
	if _, ok := s.Verify(bg(), "YXR0YWNrZXI6c3VwZXI="); ok {
		t.Fatal("forged token accepted")
	}
}

func TestExpiry(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	tok, _, _ := s.Issue(bg(), "adm1", "root", "super")
	s.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	if _, ok := s.Verify(bg(), tok); ok {
		t.Fatal("expired token accepted")
	}
}

func TestRevoke(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	tok, _, _ := s.Issue(bg(), "adm1", "root", "super")
	if err := s.Revoke(bg(), tok); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Verify(bg(), tok); ok {
		t.Fatal("revoked token accepted")
	}
}

func TestRevokeAllForAdmin(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	a, _, _ := s.Issue(bg(), "adm1", "root", "super")
	b, _, _ := s.Issue(bg(), "adm1", "root", "super")
	c, _, _ := s.Issue(bg(), "adm2", "ops", "ops")
	if err := s.RevokeAllForAdmin(bg(), "adm1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Verify(bg(), a); ok {
		t.Fatal("a still valid")
	}
	if _, ok := s.Verify(bg(), b); ok {
		t.Fatal("b still valid")
	}
	if _, ok := s.Verify(bg(), c); !ok {
		t.Fatal("c should stay valid")
	}
}

func TestEmptyTokenRejected(t *testing.T) {
	s := New(NewMemoryStore(), time.Hour)
	if _, ok := s.Verify(bg(), ""); ok {
		t.Fatal("empty token accepted")
	}
}
