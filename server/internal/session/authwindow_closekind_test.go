package session

import (
	"context"
	"testing"
	"time"
)

// ER-905 审查项 1：授权窗口过期的两个分支必须先写 reason 再 ClassifyClose，
// 否则 CloseKind 会按旧值/空串算成 unknown。
func TestAuthWindowExpirySetsCloseKindFromNewReason(t *testing.T) {
	// 分支一：ready 时窗口已过期
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, err := s.Request(context.Background(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consent(context.Background(), sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return now.Add(AuthWindow + time.Minute) })

	// ready 触发窗口检查
	_, _ = s.Ready(context.Background(), sess.ID, "c1")
	got, err := s.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateClosed || got.Reason != "auth_window_exceeded" {
		t.Fatalf("want closed/auth_window_exceeded, got %s/%q", got.State, got.Reason)
	}
	if got.CloseKind != string(CloseTimeout) {
		t.Fatalf("CloseKind must be timeout, got %q（说明先分类后写 reason）", got.CloseKind)
	}
}
