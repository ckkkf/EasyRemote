package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// 关闭后的会话不能被 Request 复活。
func TestRequestDoesNotReuseClosedSession(t *testing.T) {
	s, _ := setup(t)
	first, _ := s.Request(ctx(), "c-req1", "t-req1")
	if _, err := s.Close(ctx(), first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	second, err := s.Request(ctx(), "c-req1", "t-req1")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("closed session must not be reused")
	}
}

// 超时未授权的 requested 不能复用，且旧会话应收口为 closed。
func TestRequestDoesNotReuseExpiredRequested(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	first, _ := s.Request(ctx(), "c-req2", "t-req2")
	s.SetClock(func() time.Time { return now.Add(ConsentTimeout + time.Second) })
	second, err := s.Request(ctx(), "c-req2", "t-req2")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("expired requested must not be reused")
	}
	old, _ := s.Get(ctx(), first.ID)
	if old.State != StateClosed || old.Reason != "request_expired" {
		t.Fatalf("old session=%+v", old)
	}
}

// 授权窗口过期后不能复用 authorized 会话。
func TestRequestDoesNotReuseExpiredAuthorized(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	first, _ := s.Request(ctx(), "c-req3", "t-req3")
	if _, err := s.Consent(ctx(), first.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return now.Add(AuthWindow + time.Second) })
	second, err := s.Request(ctx(), "c-req3", "t-req3")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("expired authorized must not be reused")
	}
	old, _ := s.Get(ctx(), first.ID)
	if old.State != StateClosed {
		t.Fatalf("old state=%s, want closed", old.State)
	}
}

// 并发首次请求只能产生一个未关闭会话。
func TestConcurrentFirstRequestCreatesOne(t *testing.T) {
	s, mem := setup(t)
	const n = 16
	var wg sync.WaitGroup
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess, err := s.Request(ctx(), "c-race", "t-race")
			if err == nil {
				ids[i] = sess.ID
			}
		}(i)
	}
	wg.Wait()

	first := ""
	for _, id := range ids {
		if id == "" {
			t.Fatalf("request failed for one goroutine")
		}
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("multiple sessions created: %s vs %s", first, id)
		}
	}
	sessions, _ := mem.ListSessions(ctx(), "", store.Page{Limit: 100})
	live := 0
	for _, se := range sessions {
		if se.ControllerID == "c-race" && se.TargetID == "t-race" && se.State != StateClosed {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("live sessions=%d, want 1", live)
	}
}

func TestRequestDoesNotReuseExpiredSessionBeyondHistoryLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		age    time.Duration
		reason string
	}{
		{"requested", ConsentTimeout + time.Second, "request_expired"},
		{"authorized", AuthWindow + time.Second, "auth_window_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, mem := setup(t)
			now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
			s.SetClock(func() time.Time { return now })
			old, err := s.Request(ctx(), "controller", "target")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "authorized" {
				if _, err := s.Consent(ctx(), old.ID, true, ""); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 501; i++ {
				if err := mem.CreateSession(ctx(), &store.Session{
					ControllerID: "unrelated", TargetID: "other",
					State: StateClosed, StartedAt: now.Add(-time.Second),
				}); err != nil {
					t.Fatal(err)
				}
			}
			now = now.Add(tc.age)
			replacement, err := s.Request(ctx(), "controller", "target")
			if err != nil {
				t.Fatal(err)
			}
			if replacement.ID == old.ID || replacement.State != StateRequested {
				t.Fatalf("returned expired session: %+v", replacement)
			}
			persisted, err := s.Get(ctx(), old.ID)
			if err != nil || persisted.State != StateClosed || persisted.Reason != tc.reason {
				t.Fatalf("previous session=%+v err=%v", persisted, err)
			}
			events, err := mem.ListSessionEvents(ctx(), false, 20)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.SessionID == old.ID && event.NewState == StateClosed && event.Reason == tc.reason {
					return
				}
			}
			t.Fatal("missing durable close event")
		})
	}
}

func TestConcurrentRetryOfExpiredSessionCreatesOneReplacement(t *testing.T) {
	s, _ := setup(t)
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	first, err := s.Request(ctx(), "controller", "target")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(ConsentTimeout + time.Second)
	const count = 16
	type result struct {
		sess *store.Session
		err  error
	}
	results := make(chan result, count)
	for range count {
		go func() {
			sess, err := s.Request(ctx(), "controller", "target")
			results <- result{sess, err}
		}()
	}
	var replacementID string
	for range count {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.sess.ID == first.ID || r.sess.State != StateRequested {
			t.Fatalf("not a replacement: %+v", r.sess)
		}
		if replacementID != "" && replacementID != r.sess.ID {
			t.Fatalf("more than one replacement: %s, %s", replacementID, r.sess.ID)
		}
		replacementID = r.sess.ID
	}
}

type expiryWriteStore struct {
	store.Store
	onWrite func(context.Context, *store.Session, int64, *store.SessionEvent) error
}

func (s expiryWriteStore) UpdateSessionCASWithEvent(ctx context.Context, next *store.Session, version int64, event *store.SessionEvent) error {
	return s.onWrite(ctx, next, version, event)
}

func TestRequestExpiryDoesNotCloseConcurrentlyActivatedSession(t *testing.T) {
	s, mem := setup(t)
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	first, err := s.Request(ctx(), "controller", "target")
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := s.Consent(ctx(), first.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	now = authorized.AuthExpireAt.Add(time.Second)
	writes := 0
	s.store = expiryWriteStore{mem, func(ctx context.Context, next *store.Session, version int64, event *store.SessionEvent) error {
		writes++
		if writes == 1 {
			// 发布另一个实例的已就绪状态，同时留下当前申请的旧版本。
			active := *authorized
			active.ReadyController, active.ReadyTarget = true, true
			active.State = StateActive
			if err := mem.UpdateSessionCAS(ctx, &active, authorized.Version); err != nil {
				return err
			}
		}
		return mem.UpdateSessionCASWithEvent(ctx, next, version, event)
	}}
	got, err := s.Request(ctx(), "controller", "target")
	if err != nil || got.ID != first.ID || got.State != StateActive || writes != 1 {
		t.Fatalf("session=%+v err=%v expiry attempts=%d", got, err, writes)
	}
}

func TestRequestDoesNotReturnExpiredSessionWhenCloseWriteFails(t *testing.T) {
	s, mem := setup(t)
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	first, err := s.Request(ctx(), "controller", "target")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(ConsentTimeout + time.Second)
	s.store = expiryWriteStore{mem, func(context.Context, *store.Session, int64, *store.SessionEvent) error {
		return errors.New("test: close persistence failed")
	}}
	got, err := s.Request(ctx(), "controller", "target")
	if got != nil || err == nil || !errcode.Is(err, errcode.SrvInternal) {
		t.Fatalf("must fail closed, got=%+v err=%v", got, err)
	}
	stillPending, err := s.Get(ctx(), first.ID)
	if err != nil || stillPending.State != StateRequested {
		t.Fatalf("unexpected persisted change: %+v, %v", stillPending, err)
	}
}
