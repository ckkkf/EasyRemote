package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

func TestJanitorSkipsOnPresenceError(t *testing.T) {
	mem := store.NewMemory()
	s := New(mem)
	ctx := context.Background()
	se := &store.Session{ID: "s-err", ControllerID: "c", TargetID: "t", State: StateActive, StartedAt: time.Now().UTC(), Version: 1}
	if err := mem.CreateSession(ctx, se); err != nil {
		t.Fatal(err)
	}
	j := &Janitor{Sessions: s, Presence: fakePresence{err: errors.New("redis down")}}
	n, err := j.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("closed=%d, presence error must not mass-close", n)
	}
	got, _ := mem.GetSession(ctx, se.ID)
	if got.State != StateActive {
		t.Fatalf("state=%s", got.State)
	}
}
