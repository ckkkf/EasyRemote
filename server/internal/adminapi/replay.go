package adminapi

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// MemoryReplayGuard 是单实例实现，仅用于测试与本地开发。
type MemoryReplayGuard struct {
	mu   sync.Mutex
	used map[string]time.Time
	ttl  time.Duration
}

// NewMemoryReplayGuard 创建内存实现。
func NewMemoryReplayGuard() *MemoryReplayGuard {
	return &MemoryReplayGuard{used: map[string]time.Time{}, ttl: 5 * time.Minute}
}

// Use 标记 (subject, step) 已用；重复返回 used=false。
func (m *MemoryReplayGuard) Use(subject string, step int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for s, at := range m.used {
		if now.Sub(at) > m.ttl {
			delete(m.used, s)
		}
	}
	key := subject + "|" + itoa64(step)
	if _, ok := m.used[key]; ok {
		return false, nil
	}
	m.used[key] = now
	return true, nil
}

// RedisReplayGuard 是跨实例实现：同一时间步在 Redis 中只能 SETNX 一次。
type RedisReplayGuard struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewRedisReplayGuardFromURL 解析 redis:// 地址并校验连通。
func NewRedisReplayGuardFromURL(ctx context.Context, url string, ttl time.Duration) (*RedisReplayGuard, error) {
	rdb, err := redisClient(url)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RedisReplayGuard{rdb: rdb, ttl: ttl}, nil
}

// Use 用 SETNX 抢占 (subject, step)：成功即首次使用。
// 后端错误会返回 err，由调用方区分「重放」与「服务不可用」。
func (r *RedisReplayGuard) Use(subject string, step int64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := "er:totp:" + subject + ":" + itoa64(step)
	ok, err := r.rdb.SetNX(ctx, key, "1", r.ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// Close 关闭连接。
func (r *RedisReplayGuard) Close() error { return r.rdb.Close() }

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
