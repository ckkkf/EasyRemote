package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-02f3：时间桶趋势按小时对齐，会话与建连分别计数。
func TestTrendsBuckets(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	base := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return base.Add(3 * time.Hour) })

	// 两个会话：10:05 与 11:30；一次建连尝试在 11:40 且成功。
	for _, se := range []*store.Session{
		{ID: "ses-t1", ControllerID: "d1", TargetID: "d2", State: "closed", StartedAt: base.Add(5 * time.Minute)},
		{ID: "ses-t2", ControllerID: "d1", TargetID: "d2", State: "requested", StartedAt: base.Add(90 * time.Minute)},
	} {
		if err := mem.CreateSession(ctxb, se); err != nil {
			t.Fatal(err)
		}
	}
	if err := mem.CreateConnectAttempt(ctxb, &store.ConnectAttempt{
		SessionID: "ses-t2", AccountID: "acc1", ControllerID: "d1", TargetID: "d2",
		CreatedAt: base.Add(100 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.MarkConnectAttemptActive(ctxb, "ses-t2"); err != nil {
		t.Fatal(err)
	}

	res, err := s.Trends(ctxb, Actor{Username: "ops", Role: RoleOps}, "24h", "hour")
	if err != nil {
		t.Fatal(err)
	}
	if res.Range != "24h" || res.Bucket != "hour" || len(res.Points) != 2 {
		t.Fatalf("points: %+v", res)
	}
	if res.Points[0].SessionsStarted != 1 || res.Points[0].ConnectAttempts != 0 {
		t.Fatalf("bucket0: %+v", res.Points[0])
	}
	if res.Points[1].SessionsStarted != 1 || res.Points[1].ConnectAttempts != 1 || res.Points[1].ConnectActive != 1 {
		t.Fatalf("bucket1: %+v", res.Points[1])
	}
	// 非法参数。
	if _, err := s.Trends(ctxb, Actor{Username: "ops", Role: RoleOps}, "1y", "hour"); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want invalid range, got %v", err)
	}
	if _, err := s.Trends(ctxb, Actor{Username: "ops", Role: RoleOps}, "24h", "minute"); !errcode.Is(err, errcode.AdminConfigInvalid) {
		t.Fatalf("want invalid bucket, got %v", err)
	}
	// 权限。
	if _, err := s.Trends(ctxb, Actor{Username: "guest", Role: "guest"}, "24h", "hour"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
