package adminsession

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRedisStoreFromAddr 支持 "redis://:pass@host:port/db" 与 "host:port" 两种写法。
func newRedisStoreFromAddr(ctx context.Context, raw string, ttl time.Duration) (*RedisStore, error) {
	addr, password, db := raw, "", 0
	if strings.HasPrefix(raw, "redis://") || strings.HasPrefix(raw, "rediss://") {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		addr = u.Host
		if u.User != nil {
			password, _ = u.User.Password()
			if password == "" {
				password = u.User.Username()
			}
		}
		if p := strings.TrimPrefix(u.Path, "/"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				db = n
			}
		}
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &RedisStore{rdb: rdb, ttl: ttl}, nil
}
