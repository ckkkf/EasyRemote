package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f2：建连成功率 = 窗口内 active 尝试 / 总尝试；没有尝试时 connect_available=false。
func TestOverviewConnectRate(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}

	empty, err := s.Overview(ctxb, act)
	if err != nil {
		t.Fatal(err)
	}
	if empty.ConnectAvailable || empty.ConnectAttempts != 0 {
		t.Fatalf("want no connect data: %+v", empty)
	}

	now := time.Now().UTC()
	for _, a := range []*store.ConnectAttempt{
		{SessionID: "ses-c1", AccountID: "acc1", ControllerID: "d1", TargetID: "d2", CreatedAt: now},
		{SessionID: "ses-c2", AccountID: "acc1", ControllerID: "d1", TargetID: "d2", CreatedAt: now},
	} {
		if err := mem.CreateConnectAttempt(ctxb, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := mem.MarkConnectAttemptActive(ctxb, "ses-c1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.OverviewSince(ctxb, act, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.ConnectAvailable || got.ConnectAttempts != 2 || got.ConnectSuccessRate != 50 {
		t.Fatalf("connect stats: %+v", got)
	}
}
