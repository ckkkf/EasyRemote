package ratelimit_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/easyremote/server/internal/ratelimit"
)

// 契约：内存实现与 Redis 实现必须表现一致。
func runContract(t *testing.T, newLimiter func(t *testing.T) ratelimit.Limiter) {
	ctx := context.Background()

	t.Run("limit_enforced", func(t *testing.T) {
		l := newLimiter(t)
		for i := 0; i < 3; i++ {
			res, err := l.Allow(ctx, "c1", 3, time.Minute)
			if err != nil || !res.Allowed {
				t.Fatalf("call %d: %v %v", i+1, res, err)
			}
		}
		if res, _ := l.Allow(ctx, "c1", 3, time.Minute); res.Allowed {
			t.Fatal("must block over limit")
		}
	})

	t.Run("reset", func(t *testing.T) {
		l := newLimiter(t)
		l.Allow(ctx, "c2", 1, time.Minute)
		if err := l.Reset(ctx, "c2"); err != nil {
			t.Fatal(err)
		}
		if res, _ := l.Allow(ctx, "c2", 1, time.Minute); !res.Allowed {
			t.Fatal("after reset should pass")
		}
	})

	t.Run("namespaced_keys", func(t *testing.T) {
		l := newLimiter(t)
		l.Allow(ctx, "a", 1, time.Minute)
		if res, _ := l.Allow(ctx, "b", 1, time.Minute); !res.Allowed {
			t.Fatal("different keys must not share counters")
		}
	})
}

func TestMemoryContract(t *testing.T) {
	runContract(t, func(t *testing.T) ratelimit.Limiter { return ratelimit.NewMemory() })
}

func TestRedisContract(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	runContract(t, func(t *testing.T) ratelimit.Limiter {
		l, err := ratelimit.NewRedisFromURL(context.Background(), url, "er:rl:test:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = l.Reset(context.Background(), "c1")
			_ = l.Reset(context.Background(), "c2")
			_ = l.Reset(context.Background(), "a")
			_ = l.Reset(context.Background(), "b")
			_ = l.Close()
		})
		return l
	})
}
