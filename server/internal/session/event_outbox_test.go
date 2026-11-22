package session

import (
	"context"
	"errors"
	"testing"

	"github.com/easyremote/server/internal/store"
	"github.com/easyremote/server/internal/trace"
)

// 状态迁移必须生成 outbox 事件，字段完整且 trace 来自 context。
func TestTransitionWritesOutboxEvent(t *testing.T) {
	s, mem := setup(t)
	ctx := trace.With(context.Background(), "tr-event-1")
	sess, err := s.Request(ctx, "c-evt", "t-evt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consent(ctx, sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	events, err := mem.ListSessionEvents(context.Background(), false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	ev := events[0]
	if ev.SessionID != sess.ID || ev.FromDevice != "c-evt" || ev.ToDevice != "t-evt" {
		t.Fatalf("ids wrong: %+v", ev)
	}
	if ev.OldState != StateRequested || ev.NewState != StateAuthorized {
		t.Fatalf("states wrong: %+v", ev)
	}
	if ev.TraceID != "tr-event-1" || ev.Version != 2 || ev.At.IsZero() {
		t.Fatalf("meta wrong: %+v", ev)
	}
}

// 分发成功标记 delivered；重复分发不再投递。
func TestDispatcherDeliversOnce(t *testing.T) {
	s, mem := setup(t)
	ctx := context.Background()
	sess, _ := s.Request(ctx, "c-disp", "t-disp")
	if _, err := s.Consent(ctx, sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	d := NewDispatcher(mem, func(_ context.Context, ev *store.SessionEvent) error {
		seen[ev.ID]++
		return nil
	})
	delivered, failed, err := d.DispatchOnce(ctx, 10)
	if err != nil || delivered != 1 || failed != 0 {
		t.Fatalf("delivered=%d failed=%d err=%v", delivered, failed, err)
	}
	delivered, _, err = d.DispatchOnce(ctx, 10)
	if err != nil || delivered != 0 {
		t.Fatalf("second dispatch delivered=%d err=%v", delivered, err)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("event %s handled %d times", id, n)
		}
	}
}

// 消费失败保留事件并累计 attempts；重试成功后标记 delivered。
func TestDispatcherRetriesFailedEvent(t *testing.T) {
	s, mem := setup(t)
	ctx := context.Background()
	sess, _ := s.Request(ctx, "c-retry", "t-retry")
	if _, err := s.Consent(ctx, sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	fail := true
	d := NewDispatcher(mem, func(_ context.Context, _ *store.SessionEvent) error {
		if fail {
			return errors.New("downstream down")
		}
		return nil
	})
	delivered, failed, err := d.DispatchOnce(ctx, 10)
	if err != nil || delivered != 0 || failed != 1 {
		t.Fatalf("delivered=%d failed=%d err=%v", delivered, failed, err)
	}
	pending, _ := mem.ListSessionEvents(ctx, false, 10)
	if len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError != "downstream down" {
		t.Fatalf("pending=%+v", pending)
	}
	fail = false
	delivered, failed, err = d.DispatchOnce(ctx, 10)
	if err != nil || delivered != 1 || failed != 0 {
		t.Fatalf("retry delivered=%d failed=%d err=%v", delivered, failed, err)
	}
}
