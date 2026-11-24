package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-03g：会话详情返回双方设备、链路切换与状态时间线。
func TestSessionDetailPartiesAndSwitches(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	devices := []*store.Device{
		{ID: "da", DeviceCode: "997000001", AccountID: "acc1", Platform: "macOS", OSVersion: "macOS 15", ClientVersion: "0.2.13", Online: true},
		{ID: "db", DeviceCode: "997000002", AccountID: "acc1", Platform: "Windows", OSVersion: "Win 11", ClientVersion: "0.2.13", Online: false},
	}
	for _, d := range devices {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	se, err := sess.Request(ctxb, "da", "db")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: se.ID, TS: base.Add(-3 * time.Second), LatencyMs: 20, Link: "p2p"},
		{SessionID: se.ID, TS: base.Add(-2 * time.Second), LatencyMs: 25, Link: "p2p"},
		{SessionID: se.ID, TS: base.Add(-time.Second), LatencyMs: 40, Link: "relay"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Kill(ctxb, se.ID, "user hangup"); err != nil {
		t.Fatal(err)
	}

	detail, err := s.SessionDetail(ctxb, act, se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Controller.DeviceCode != "997000001" || detail.Target.OsVersion != "Win 11" {
		t.Fatalf("parties: %+v", detail)
	}
	if len(detail.LinkSwitches) != 1 || detail.LinkSwitches[0].From != "p2p" || detail.LinkSwitches[0].To != "relay" {
		t.Fatalf("switches: %+v", detail.LinkSwitches)
	}
	if len(detail.Timeline) != 2 || detail.Timeline[1].Detail != "user hangup" {
		t.Fatalf("timeline: %+v", detail.Timeline)
	}
}
