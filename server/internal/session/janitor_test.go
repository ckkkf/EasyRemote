package session

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

type fakePresence struct {
	online map[string]bool
	err    error
}

func (f fakePresence) Online(_ context.Context, id string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.online[id], nil
}

type fakeCloser struct{ closed []string }

func (f *fakeCloser) NotifySessionClosed(id, _, _, _ string) error {
	f.closed = append(f.closed, id)
	return nil
}

// ER-903a：双方/一方离线后，active 必须被 janitor 关闭并传播，不能永久 active。
func TestJanitorClosesActiveWhenPeerOffline(t *testing.T) {
	mem := store.NewMemory()
	s := New(mem)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, sess := range []*store.Session{
		{ID: "s-off", ControllerID: "c-off", TargetID: "t-off", State: StateActive, StartedAt: now, Version: 1},
		{ID: "s-on", ControllerID: "c-on", TargetID: "t-on", State: StateActive, StartedAt: now, Version: 1},
	} {
		if err := mem.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	closer := &fakeCloser{}
	j := &Janitor{
		Sessions: s,
		Presence: fakePresence{online: map[string]bool{
			"c-off": false, "t-off": true,
			"c-on": true, "t-on": true,
		}},
		Closer: closer,
	}
	n, err := j.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("closed=%d want 1", n)
	}
	got, _ := mem.GetSession(ctx, "s-off")
	if got.State != StateClosed || got.Reason != "peer_offline" {
		t.Fatalf("offline session=%+v", got)
	}
	on, _ := mem.GetSession(ctx, "s-on")
	if on.State != StateActive {
		t.Fatalf("online session must stay active, got %s", on.State)
	}
	if len(closer.closed) != 1 || closer.closed[0] != "s-off" {
		t.Fatalf("closer=%+v", closer.closed)
	}
}
