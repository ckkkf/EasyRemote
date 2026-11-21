package relay

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRegistry 是跨节点目录实现。
type RedisRegistry struct {
	rdb *redis.Client
}

var _ Registry = (*RedisRegistry)(nil)

// NewRedisRegistryFromURL 解析 redis:// 或 host:port 并校验连通。
func NewRedisRegistryFromURL(ctx context.Context, raw string) (*RedisRegistry, error) {
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
		return nil, fmt.Errorf("relay: redis ping failed: %w", err)
	}
	return &RedisRegistry{rdb: rdb}, nil
}

// Close 关闭连接。
func (r *RedisRegistry) Close() error { return r.rdb.Close() }

func nodeKey(nodeID string) string         { return "er:relay:node:" + nodeID }
func routeKeyRedis(sid, dev string) string { return "er:relay:route:" + sid + ":" + dev }

func (r *RedisRegistry) RegisterNode(ctx context.Context, nodeID, addr string, ttl time.Duration) error {
	return r.rdb.Set(ctx, nodeKey(nodeID), addr, ttl).Err()
}

func (r *RedisRegistry) LookupNode(ctx context.Context, nodeID string) (string, error) {
	v, err := r.rdb.Get(ctx, nodeKey(nodeID)).Result()
	if err == redis.Nil {
		return "", ErrShort
	}
	return v, err
}

func (r *RedisRegistry) SetRoute(ctx context.Context, sessionID, deviceID, nodeID string, ttl time.Duration) error {
	return r.rdb.Set(ctx, routeKeyRedis(sessionID, deviceID), nodeID, ttl).Err()
}

func (r *RedisRegistry) LookupRoute(ctx context.Context, sessionID, deviceID string) (string, error) {
	v, err := r.rdb.Get(ctx, routeKeyRedis(sessionID, deviceID)).Result()
	if err == redis.Nil {
		return "", ErrShort
	}
	return v, err
}

func (r *RedisRegistry) ClearRoute(ctx context.Context, sessionID, deviceID string) error {
	return r.rdb.Del(ctx, routeKeyRedis(sessionID, deviceID)).Err()
}

func (r *RedisRegistry) Ping(ctx context.Context) error { return r.rdb.Ping(ctx).Err() }
