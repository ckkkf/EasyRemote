package adminsession

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 把后台会话放在 Redis，支持多实例部署与即时撤销。
//
// key 设计：
//
//	er:adminsess:tok:<token>   → hash（会话字段），TTL = 会话有效期
//	er:adminsess:adm:<adminID> → set（该账号的全部 token），用于批量撤销
type RedisStore struct {
	rdb *redis.Client
	ttl time.Duration
}

var _ Store = (*RedisStore)(nil)

// NewRedisStore 创建 Redis 存储并校验连通性。
func NewRedisStore(ctx context.Context, addr, password string, db int, ttl time.Duration) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("adminsession: redis ping failed: %w", err)
	}
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &RedisStore{rdb: rdb, ttl: ttl}, nil
}

// Close 关闭连接。
func (r *RedisStore) Close() error { return r.rdb.Close() }

func tokenKey(token string) string   { return "er:adminsess:tok:" + token }
func adminKey(adminID string) string { return "er:adminsess:adm:" + adminID }

// Put 写入会话并设置 TTL（TTL 到期即等价于过期）。
func (r *RedisStore) Put(ctx context.Context, s *Session) error {
	ttl := time.Until(s.ExpiresAt)
	if ttl <= 0 {
		ttl = r.ttl
	}
	pipe := r.rdb.TxPipeline()
	pipe.HSet(ctx, tokenKey(s.Token), map[string]any{
		"admin_id":   s.AdminID,
		"username":   s.Username,
		"role":       s.Role,
		"created_at": s.CreatedAt.Unix(),
		"expires_at": s.ExpiresAt.Unix(),
		"revoked":    boolInt(s.Revoked),
	})
	pipe.Expire(ctx, tokenKey(s.Token), ttl)
	pipe.SAdd(ctx, adminKey(s.AdminID), s.Token)
	pipe.Expire(ctx, adminKey(s.AdminID), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func boolInt(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// Get 读取会话。key 不存在返回 ErrSessionNotFound。
func (r *RedisStore) Get(ctx context.Context, token string) (*Session, error) {
	data, err := r.rdb.HGetAll(ctx, tokenKey(token)).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrSessionNotFound
	}
	created, _ := strconv.ParseInt(data["created_at"], 10, 64)
	expires, _ := strconv.ParseInt(data["expires_at"], 10, 64)
	return &Session{
		Token:     token,
		AdminID:   data["admin_id"],
		Username:  data["username"],
		Role:      data["role"],
		CreatedAt: time.Unix(created, 0).UTC(),
		ExpiresAt: time.Unix(expires, 0).UTC(),
		Revoked:   data["revoked"] == "1",
	}, nil
}

// Revoke 把 revoked 置 1 并保留剩余 TTL（不立刻删除，便于审计期内可查）。
func (r *RedisStore) Revoke(ctx context.Context, token string) error {
	exists, err := r.rdb.Exists(ctx, tokenKey(token)).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	return r.rdb.HSet(ctx, tokenKey(token), "revoked", "1").Err()
}

// RevokeAllForAdmin 撤销某账号的全部会话。
func (r *RedisStore) RevokeAllForAdmin(ctx context.Context, adminID string) error {
	tokens, err := r.rdb.SMembers(ctx, adminKey(adminID)).Result()
	if err != nil {
		return err
	}
	for _, t := range tokens {
		if err := r.Revoke(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

// Ping 健康检查。
func (r *RedisStore) Ping(ctx context.Context) error { return r.rdb.Ping(ctx).Err() }
