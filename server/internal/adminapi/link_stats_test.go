package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f4：P2P 命中率与帧率达成率只统计客户端上报过的采样。
func TestOverviewLinkAndFpsRates(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	now := time.Now().UTC().Truncate(time.Second)

	empty, err := s.Overview(ctxb, act)
	if err != nil {
		t.Fatal(err)
	}
	if empty.LinkAvailable || empty.FpsAvailable {
		t.Fatalf("want no link/fps data: %+v", empty)
	}

	// 4 条采样：2 p2p、2 relay；其中 3 条带 target_fps，2 条达成。
	samples := []store.QualitySample{
		{SessionID: "s1", TS: now, LatencyMs: 20, Fps: 60, Link: "p2p", TargetFps: 60},
		{SessionID: "s1", TS: now.Add(-time.Second), LatencyMs: 22, Fps: 40, Link: "p2p", TargetFps: 60},
		{SessionID: "s1", TS: now.Add(-2 * time.Second), LatencyMs: 30, Fps: 60, Link: "relay", TargetFps: 60},
		{SessionID: "s1", TS: now.Add(-3 * time.Second), LatencyMs: 35, Fps: 30, Link: "relay"},
	}
	if err := mem.AppendQualitySamples(ctxb, samples); err != nil {
		t.Fatal(err)
	}
	got, err := s.OverviewSince(ctxb, act, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.LinkAvailable || got.P2pHitRate != 50 {
		t.Fatalf("p2p: %+v", got)
	}
	if !got.FpsAvailable || got.FpsTargetRate != 67 {
		t.Fatalf("fps: %+v", got)
	}
}
