package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryAllowsUpToLimit(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		res, err := l.Allow(ctx, "k", 3, time.Minute)
		if err != nil || !res.Allowed {
			t.Fatalf("call %d should pass: %v %v", i+1, res, err)
		}
	}
	res, _ := l.Allow(ctx, "k", 3, time.Minute)
	if res.Allowed {
		t.Fatal("4th call must be blocked")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("retry-after should be positive")
	}
}

func TestMemoryWindowResets(t *testing.T) {
	l := NewMemory()
	now := time.Now().UTC()
	l.SetClock(func() time.Time { return now })
	ctx := context.Background()
	l.Allow(ctx, "k", 1, time.Minute)
	if res, _ := l.Allow(ctx, "k", 1, time.Minute); res.Allowed {
		t.Fatal("should be blocked within window")
	}
	l.SetClock(func() time.Time { return now.Add(2 * time.Minute) })
	if res, _ := l.Allow(ctx, "k", 1, time.Minute); !res.Allowed {
		t.Fatal("should pass after window")
	}
}

func TestMemoryConcurrentNoLostUpdates(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	const n = 100
	var allowed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _ := l.Allow(ctx, "k", 10, time.Minute)
			if res.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 10 {
		t.Fatalf("want exactly 10 allowed, got %d", allowed)
	}
}

func TestReset(t *testing.T) {
	l := NewMemory()
	ctx := context.Background()
	l.Allow(ctx, "k", 1, time.Minute)
	if err := l.Reset(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if res, _ := l.Allow(ctx, "k", 1, time.Minute); !res.Allowed {
		t.Fatal("should pass after reset")
	}
}
