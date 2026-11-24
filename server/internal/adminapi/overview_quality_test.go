package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02e：概览的质量指标来自真实采样；无采样时 quality_available=false。
func TestOverviewQualityMetrics(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}

	empty, err := s.Overview(ctxb, act)
	if err != nil {
		t.Fatal(err)
	}
	if empty.QualityAvailable || empty.QualitySamples != 0 {
		t.Fatalf("want no quality data: %+v", empty)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: "ses-o1", TS: now, LatencyMs: 20},
		{SessionID: "ses-o1", TS: now.Add(-time.Second), LatencyMs: 40, Relay: true},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.OverviewSince(ctxb, act, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.QualityAvailable || got.QualitySamples != 2 {
		t.Fatalf("quality: %+v", got)
	}
	if got.LatencyP50Ms != 20 || got.LatencyP95Ms != 40 || got.RelayPercent != 50 {
		t.Fatalf("percentiles: %+v", got)
	}
}
