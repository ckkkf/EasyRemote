package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/relay"
	"github.com/easyremote/server/internal/relaymgr"
)

// 重连换票：新票代号递增，旧票代号被拒。
func TestStaleTicketRejectedAfterRotation(t *testing.T) {
	url := startRedis(t)
	ctx := context.Background()
	reg, err := relay.NewRedisRegistryFromURL(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	svc := relaymgr.New([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	e1, err := reg.NextTicketEpoch(ctx, "ses-rot", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	tok1, tk1, err := svc.IssueWithEpoch("ses-rot", "dev-a", "dev-b", e1)
	if err != nil {
		t.Fatal(err)
	}
	verified1, err := svc.Verify(tok1)
	if err != nil {
		t.Fatal(err)
	}
	if verified1.Epoch != e1 {
		t.Fatalf("epoch=%d want %d", verified1.Epoch, e1)
	}
	if !relay.TicketEpochCurrent(ctx, reg, tk1) {
		t.Fatal("first ticket must be current")
	}

	e2, err := reg.NextTicketEpoch(ctx, "ses-rot", "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if e2 != e1+1 {
		t.Fatalf("epoch=%d want %d", e2, e1+1)
	}
	if relay.TicketEpochCurrent(ctx, reg, tk1) {
		t.Fatal("old ticket must be rejected after rotation")
	}
}
