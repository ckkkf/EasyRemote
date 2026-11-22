package session

import (
	"sync"
	"testing"
	"time"

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
