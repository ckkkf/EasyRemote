package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f1：异常中断率按结束原因归一统计；窗口内无结束会话时 stability_available=false。
func TestOverviewStabilityMetrics(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}

	empty, err := s.Overview(ctxb, act)
	if err != nil {
		t.Fatal(err)
	}
	if empty.StabilityAvailable || empty.ClosedSessions != 0 {
		t.Fatalf("want no stability data: %+v", empty)
	}

	// 造两台设备与两次会话：一次用户挂断（正常），一次网络中断（异常）。
	devices := []*store.Device{
		{ID: "dev-a", DeviceCode: "981000001", AccountID: "acc1", Platform: "macOS"},
		{ID: "dev-b", DeviceCode: "981000002", AccountID: "acc1", Platform: "Windows"},
	}
	for _, d := range devices {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	normal, err := sess.Request(ctxb, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Kill(ctxb, normal.ID, "user hangup"); err != nil {
		t.Fatal(err)
	}
	abnormal, err := sess.Request(ctxb, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Kill(ctxb, abnormal.ID, "network changed"); err != nil {
		t.Fatal(err)
	}

	got, err := s.OverviewSince(ctxb, act, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.StabilityAvailable || got.ClosedSessions != 2 || got.AbnormalCloses != 1 {
		t.Fatalf("stability counts: %+v", got)
	}
	if got.AbnormalCloseRate != 50 {
		t.Fatalf("abnormal rate: %d", got.AbnormalCloseRate)
	}
}
