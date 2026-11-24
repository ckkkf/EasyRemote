package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f5：崩溃率 = 崩溃/窗口内会话数；重连成功率 = 成功/尝试。
func TestOverviewHealthMetrics(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	now := time.Now().UTC()

	empty, err := s.Overview(ctxb, act)
	if err != nil {
		t.Fatal(err)
	}
	if empty.CrashAvailable || empty.ReconnectAvailable {
		t.Fatalf("want no health data: %+v", empty)
	}

	// 窗口内一个会话；崩溃 1 次、重连尝试 2 次、成功 1 次。
	if err := mem.CreateSession(ctxb, &store.Session{
		ID: "ses-h", ControllerID: "d1", TargetID: "d2", State: "requested", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"crash", "reconnect_attempt", "reconnect_attempt", "reconnect_success"} {
		if err := mem.RecordHealthEvent(ctxb, &store.HealthEvent{
			SessionID: "ses-h", DeviceID: "d1", Kind: kind, TS: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.OverviewSince(ctxb, act, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.CrashAvailable || got.CrashRate != 100 {
		t.Fatalf("crash: %+v", got)
	}
	if !got.ReconnectAvailable || got.ReconnectSuccessRate != 50 {
		t.Fatalf("reconnect: %+v", got)
	}
}
