package ratelimit

import (
	"context"
	"testing"
	"time"
)

// 开发用无限流器必须在任意次数下都放行，否则本地联调仍会被锁。
func TestUnlimitedAlwaysAllows(t *testing.T) {
	lim := NewUnlimited()
	for i := 0; i < 100; i++ {
		res, err := lim.Allow(context.Background(), "login:phone:13800000000", 10, time.Hour)
		if err != nil {
			t.Fatalf("allow %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("attempt %d blocked: %+v", i, res)
		}
	}
}

func TestUnlimitedResetAndPing(t *testing.T) {
	lim := NewUnlimited()
	if err := lim.Reset(context.Background(), "adminlogin:root"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := lim.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
