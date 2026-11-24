package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f7：会话列表带平均延迟与主导链路；无采样时 latency_available=false。
func TestSessionRowsCarryQualitySummary(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	for _, d := range []*store.Device{
		{ID: "dq1", DeviceCode: "994000001", AccountID: "acc1", Platform: "macOS"},
		{ID: "dq2", DeviceCode: "994000002", AccountID: "acc1", Platform: "Windows"},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	se, err := sess.Request(ctxb, "dq1", "dq2")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: se.ID, TS: now, LatencyMs: 20, Link: "p2p"},
		{SessionID: se.ID, TS: now.Add(-time.Second), LatencyMs: 40, Link: "p2p"},
		{SessionID: se.ID, TS: now.Add(-2 * time.Second), LatencyMs: 60, Link: "relay"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ID != se.ID {
			continue
		}
		found = true
		if !r.LatencyAvailable || r.AvgLatencyMs != 40 || r.Link != "p2p" {
			t.Fatalf("summary: %+v", r)
		}
	}
	if !found {
		t.Fatal("session row not found")
	}
}
