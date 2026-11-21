package adminsession

import (
	"context"
	"time"
)

// NewRedisStoreFromURL 用 redis:// 或 host:port 形式的地址创建存储。
func NewRedisStoreFromURL(ctx context.Context, url string, ttl time.Duration) (*RedisStore, error) {
	return newRedisStoreFromAddr(ctx, url, ttl)
}

// FlushTestKeys 只清理本服务的 key，避免污染同一 Redis 上的其他数据。
func (r *RedisStore) FlushTestKeys(ctx context.Context) error {
	iter := r.rdb.Scan(ctx, 0, "er:adminsess:*", 100).Iterator()
	for iter.Next(ctx) {
		if err := r.rdb.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}
