package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-02d：设备质量聚合的 available 语义与权限。
func TestDeviceQualityAggregates(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "cs", Role: RoleCS}

	none, err := s.DeviceQuality(ctxb, act, "dev-none", 30)
	if err != nil || none.Available || len(none.Days) != 0 {
		t.Fatalf("want unavailable: %v %+v", err, none)
	}

	d1 := &store.Device{DeviceCode: "950000001", AccountID: "acc1", Platform: "macOS"}
	d2 := &store.Device{DeviceCode: "950000002", AccountID: "acc1", Platform: "Windows"}
	for _, d := range []*store.Device{d1, d2} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	sess := &store.Session{ID: "ses-dq", ControllerID: d1.ID, TargetID: d2.ID, State: "closed", StartedAt: time.Now().UTC()}
	if err := mem.CreateSession(ctxb, sess); err != nil {
		t.Fatal(err)
	}
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: sess.ID, TS: time.Now().UTC().Truncate(time.Second), LatencyMs: 33, LossPermille: 3, Fps: 59, Relay: true},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.DeviceQuality(ctxb, act, d1.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Available || len(res.Days) != 1 || res.Days[0].AvgLatencyMs != 33 || res.Days[0].RelayPercent != 100 {
		t.Fatalf("aggregate: %+v", res)
	}
	if _, err := s.DeviceQuality(ctxb, Actor{Username: "guest", Role: "guest"}, d1.ID, 30); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
