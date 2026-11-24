package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-02c：没有采样时 available=false；有采样时返回真实样本。
func TestSessionQualityAvailableFlag(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "cs", Role: RoleCS}

	empty, err := s.SessionQuality(ctxb, act, "ses-none")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Available || len(empty.Samples) != 0 {
		t.Fatalf("want unavailable for no samples, got %+v", empty)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := mem.AppendQualitySamples(ctxb, []store.QualitySample{
		{SessionID: "ses-q", TS: now, LatencyMs: 28, LossPermille: 1, Fps: 60, Tier: "hd"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionQuality(ctxb, act, "ses-q")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || len(got.Samples) != 1 || got.Samples[0].LatencyMs != 28 {
		t.Fatalf("want available with 1 sample, got %+v", got)
	}
	// 未知角色被拒。
	if _, err := s.SessionQuality(ctxb, Actor{Username: "guest", Role: "guest"}, "ses-q"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
