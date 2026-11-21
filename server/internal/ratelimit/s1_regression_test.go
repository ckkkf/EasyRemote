package ratelimit_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/easyremote/server/internal/ratelimit"
	"github.com/redis/go-redis/v9"
)

// S1 回归：历史遗留的无 TTL key 必须在一次 Allow 后补上过期时间，
// 而不是永久拒绝该主体。
func TestRedisKeyWithoutTTLGetsExpiry(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	ctx := context.Background()
	l, err := ratelimit.NewRedisFromURL(ctx, url, "er:rl:s1:")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	// 手工造一个无 TTL 且已超限的 key
	raw, err := redisClientForTest(url)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	key := "er:rl:s1:stale"
	raw.Set(ctx, key, 99, 0) // 无过期
	defer raw.Del(ctx, key)

	res, err := l.Allow(ctx, "stale", 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("over-limit stale key should still be blocked in this window")
	}
	if res.RetryAfter <= 0 {
		t.Fatalf("RetryAfter must be positive, got %v", res.RetryAfter)
	}
	// 关键断言：TTL 已被补上
	ttl := raw.PTTL(ctx, key).Val()
	if ttl <= 0 {
		t.Fatalf("stale key must get a TTL, got %v", ttl)
	}
}

func TestInvalidParamsRejected(t *testing.T) {
	l := ratelimit.NewMemory()
	ctx := context.Background()
	if _, err := l.Allow(ctx, "k", 0, time.Minute); err == nil {
		t.Fatal("limit=0 must error, not silently disable")
	}
	if _, err := l.Allow(ctx, "k", 10, 0); err == nil {
		t.Fatal("window=0 must error")
	}
}

func redisClientForTest(url string) (*redis.Client, error) {
	return ratelimit.TestClient(url)
}
