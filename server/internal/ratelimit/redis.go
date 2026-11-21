package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// allowScript 用 Lua 保证 INCR + EXPIRE 的原子性：
// 首次计数时设置窗口 TTL，后续只自增，避免"设置过期失败导致 key 永不过期"。
var allowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
-- 首次计数，或历史遗留的无 TTL key（ttl < 0），都要补设过期，
-- 否则该 key 永久存在，主体被永久封禁（审查项 S1）。
if current == 1 or ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  ttl = redis.call('PTTL', KEYS[1])
end
return {current, ttl}
`)

// Redis 是跨实例限流实现。
type Redis struct {
	rdb    *redis.Client
	prefix string
}

var _ Limiter = (*Redis)(nil)

// NewRedisFromURL 支持 redis://:pass@host:port/db 与 host:port。
func NewRedisFromURL(ctx context.Context, url string, prefix string) (*Redis, error) {
	rdb, err := clientFromURL(url)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	if prefix == "" {
		prefix = "er:rl:"
	}
	return &Redis{rdb: rdb, prefix: prefix}, nil
}

// Close 关闭连接。
func (r *Redis) Close() error { return r.rdb.Close() }

func (r *Redis) Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	if window <= 0 {
		return Result{}, fmt.Errorf("ratelimit: window must be positive")
	}
	if limit <= 0 {
		// 配置漏填时宁可报错，也不要静默关掉防护
		return Result{}, fmt.Errorf("ratelimit: limit must be positive")
	}
	raw, err := allowScript.Run(ctx, r.rdb, []string{r.prefix + key}, limit, window.Milliseconds()).Slice()
	if err != nil {
		return Result{}, err
	}
	if len(raw) < 2 {
		return Result{}, fmt.Errorf("ratelimit: unexpected script reply")
	}
	current := toInt(raw[0])
	ttlMs := toInt(raw[1])
	if current > int64(limit) {
		retry := time.Duration(ttlMs) * time.Millisecond
		if retry <= 0 {
			retry = time.Second
		}
		return Result{Allowed: false, Remaining: 0, RetryAfter: retry}, nil
	}
	return Result{Allowed: true, Remaining: limit - int(current)}, nil
}

func (r *Redis) Reset(ctx context.Context, key string) error {
	return r.rdb.Del(ctx, r.prefix+key).Err()
}

func (r *Redis) Ping(ctx context.Context) error { return r.rdb.Ping(ctx).Err() }

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case string:
		var out int64
		for _, c := range n {
			if c < '0' || c > '9' {
				return out
			}
			out = out*10 + int64(c-'0')
		}
		return out
	default:
		return 0
	}
}
