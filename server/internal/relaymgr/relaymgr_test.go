package relaymgr

import (
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
)

func svc() *Service { return New([]byte("0123456789abcdef"), time.Hour) }

func TestIssueAndVerify(t *testing.T) {
	s := svc()
	tok, tk, err := s.Issue("s1", "devA", "devB")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "s1" || got.Self != "devA" || got.Peer != "devB" {
		t.Fatalf("mismatch: %+v", got)
	}
	if tk.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatal("already expired")
	}
}

func TestTicketIsDeviceBound(t *testing.T) {
	s := svc()
	tokA, _, _ := s.Issue("s1", "devA", "devB")
	tokB, _, _ := s.Issue("s1", "devB", "devA")
	if tokA == tokB {
		t.Fatal("per-device tickets must differ")
	}
	a, _ := s.Verify(tokA)
	b, _ := s.Verify(tokB)
	if a.Self == b.Self {
		t.Fatal("tickets must bind different self ids")
	}
}

func TestTamperedRejected(t *testing.T) {
	s := svc()
	tok, _, _ := s.Issue("s1", "a", "b")
	parts := strings.Split(tok, ".")
	if _, err := s.Verify(parts[0] + "." + parts[1] + "x"); !errcode.Is(err, errcode.ConnRelayUnavailable) {
		t.Fatalf("want rejected, got %v", err)
	}
}

func TestExpired(t *testing.T) {
	s := New([]byte("0123456789abcdef"), time.Minute)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	tok, _, _ := s.Issue("s1", "a", "b")
	s.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	if _, err := s.Verify(tok); !errcode.Is(err, errcode.ConnRelayUnavailable) {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestEmptySecretRejected(t *testing.T) {
	s := New(nil, time.Hour)
	if err := s.Validate(); err == nil {
		t.Fatal("empty secret must be rejected")
	}
	if _, _, err := s.Issue("s1", "a", "b"); err == nil {
		t.Fatal("issue with empty secret must fail")
	}
}
