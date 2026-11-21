package signal

import (
	"sync"
	"testing"
	"time"
)

type fakeConn struct {
	mu   sync.Mutex
	msgs []struct {
		Type int
		Body any
	}
	closed bool
}

func (f *fakeConn) Send(t int, body any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, struct {
		Type int
		Body any
	}{t, body})
	return nil
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeConn) last() (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.msgs) == 0 {
		return 0, false
	}
	return f.msgs[len(f.msgs)-1].Type, true
}

func TestRegisterReplacesOldConn(t *testing.T) {
	h := NewHub()
	old := &fakeConn{}
	if replaced := h.Register("devA", old); replaced != nil {
		t.Fatal("first register should not replace")
	}
	cur := &fakeConn{}
	if replaced := h.Register("devA", cur); replaced != old {
		t.Fatal("duplicate register must return old conn")
	}
}

func TestUnregisterOnlyRemovesOwnConn(t *testing.T) {
	h := NewHub()
	old := &fakeConn{}
	h.Register("devA", old)
	cur := &fakeConn{}
	h.Register("devA", cur)

	// 旧连接关闭不得把新连接踢掉（审查项 S12-4）
	if h.Unregister("devA", old) {
		t.Fatal("stale conn must not unregister the new one")
	}
	if !h.Online("devA") {
		t.Fatal("devA should still be online")
	}
	if !h.Unregister("devA", cur) {
		t.Fatal("owner conn should unregister")
	}
	if h.Online("devA") {
		t.Fatal("devA should be offline")
	}
}

func TestBindRequiresOnlineSender(t *testing.T) {
	h := NewHub()
	if _, err := h.Bind("s1", "devA", "devB"); err != ErrNotBound {
		t.Fatalf("want ErrNotBound, got %v", err)
	}
	h.Register("devA", &fakeConn{})
	if _, err := h.Bind("s1", "devA", "devB"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestBindRejectsSelf(t *testing.T) {
	h := NewHub()
	h.Register("devA", &fakeConn{})
	if _, err := h.Bind("s1", "devA", "devA"); err != ErrNotBound {
		t.Fatalf("self bind must fail, got %v", err)
	}
}

func TestCandidatesOnlyBetweenBoundParties(t *testing.T) {
	h := NewHub()
	a, b, third := &fakeConn{}, &fakeConn{}, &fakeConn{}
	h.Register("devA", a)
	h.Register("devB", b)
	h.Register("devC", third)
	h.Bind("s1", "devA", "devB")

	if err := h.ForwardCandidates("s1", "devA", []any{"cand"}); err != nil {
		t.Fatalf("legal forward failed: %v", err)
	}
	if typ, _ := b.last(); typ != TypeCandidates {
		t.Fatalf("B should receive candidates, got %d", typ)
	}

	// 第三方不得向会话注入候选
	if err := h.ForwardCandidates("s1", "devC", []any{"evil"}); err == nil {
		t.Fatal("third party injection must fail")
	}
	// 未绑定会话不得转发
	if err := h.ForwardCandidates("s2", "devA", []any{"x"}); err == nil {
		t.Fatal("unbound session must fail")
	}
}

func TestConsentOnlyFromTarget(t *testing.T) {
	h := NewHub()
	a, b := &fakeConn{}, &fakeConn{}
	h.Register("devA", a)
	h.Register("devB", b)
	h.Bind("s1", "devA", "devB")

	// 主控不能替被控端授权
	if err := h.ForwardConsent("s1", "devA", true); err == nil {
		t.Fatal("controller must not consent for target")
	}
	if err := h.ForwardConsent("s1", "devB", true); err != nil {
		t.Fatalf("target consent failed: %v", err)
	}
	if typ, _ := a.last(); typ != TypeConsentGranted {
		t.Fatalf("A should get granted, got %d", typ)
	}
	if err := h.ForwardConsent("s1", "devB", false); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if typ, _ := a.last(); typ != TypeDisconnect {
		t.Fatalf("A should get disconnect, got %d", typ)
	}
}

func TestNotifyConsentRequired(t *testing.T) {
	h := NewHub()
	a, b := &fakeConn{}, &fakeConn{}
	h.Register("devA", a)
	h.Register("devB", b)
	h.Bind("s1", "devA", "devB")
	if err := h.NotifyConsentRequired("s1", "devA", "devB"); err != nil {
		t.Fatal(err)
	}
	if typ, _ := b.last(); typ != TypeConsentRequired {
		t.Fatalf("B should be asked, got %d", typ)
	}
}

func TestDisconnectClearsBinding(t *testing.T) {
	h := NewHub()
	a, b := &fakeConn{}, &fakeConn{}
	h.Register("devA", a)
	h.Register("devB", b)
	h.Bind("s1", "devA", "devB")
	if err := h.Disconnect("s1", "devA", "bye"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Binding("s1"); ok {
		t.Fatal("binding should be cleared")
	}
}

func TestCounts(t *testing.T) {
	h := NewHub()
	h.Register("devA", &fakeConn{})
	h.Register("devB", &fakeConn{})
	h.Bind("s1", "devA", "devB")
	d, s := h.Counts()
	if d != 2 || s != 1 {
		t.Fatalf("counts=%d,%d", d, s)
	}
	_ = time.Now
}
