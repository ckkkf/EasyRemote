package adminapi

import (
	"sync"
	"testing"
)

type adminFakeCloser struct {
	mu   sync.Mutex
	args []struct{ sid, from, to, reason string }
}

func (f *adminFakeCloser) NotifySessionClosed(sid, from, to, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args = append(f.args, struct{ sid, from, to, reason string }{sid, from, to, reason})
	return nil
}

// 后台强制中断也必须把关闭事件传播给双方。
func TestAdminKillPropagatesClose(t *testing.T) {
	s, mem, sess := setup(t)
	closer := &adminFakeCloser{}
	s.SetSessionCloser(closer)
	act := Actor{Username: "cs", Role: RoleCS, IP: "10.0.0.9"}
	se, err := sess.Request(ctx(), "c-close", "t-close")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Consent(ctx(), se.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.KillSession(ctx(), act, se.ID, "abuse"); err != nil {
		t.Fatal(err)
	}
	got, _ := sess.Get(ctx(), se.ID)
	if got.State != "closed" {
		t.Fatalf("state=%s", got.State)
	}
	if len(closer.args) != 1 {
		t.Fatalf("closer calls=%d", len(closer.args))
	}
	args := closer.args[0]
	if args.sid != se.ID || args.from != "c-close" || args.to != "t-close" || args.reason != "admin: abuse" {
		t.Fatalf("closer args=%+v", args)
	}
	if len(listAudit(mem)) != 1 {
		t.Fatalf("audit count=%d", len(listAudit(mem)))
	}
}
