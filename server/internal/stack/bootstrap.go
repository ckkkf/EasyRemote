package stack

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/store"
)

// ErrDatabaseRequired 表示生产环境缺少 DATABASE_URL。
var ErrDatabaseRequired = errors.New("stack: DATABASE_URL is required in production")

// OpenStore 按环境打开存储：
//   - 有 DATABASE_URL → PostgreSQL（自动迁移）
//   - 生产环境但没配 → 直接失败（不允许生产跑内存）
//   - 开发环境 → 内存（仅本地联调）
func OpenStore(ctx context.Context) (store.Store, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn != "" {
		pg, err := store.NewPG(ctx, dsn)
		if err != nil {
			return nil, err
		}
		ddl, err := LoadMigration()
		if err != nil {
			return nil, err
		}
		if err := pg.Migrate(ctx, ddl); err != nil {
			_ = pg.Close()
			return nil, err
		}
		return pg, nil
	}
	if config.IsProduction() {
		return nil, ErrDatabaseRequired
	}
	return store.NewMemory(), nil
}

// OpenSessions 按环境打开后台会话后端：
//   - 有 REDIS_URL → Redis（多实例可用）
//   - 生产环境但没配 → 直接失败
//   - 开发环境 → 内存
func OpenSessions(ctx context.Context, ttl time.Duration) (*adminsession.Service, error) {
	url := os.Getenv("REDIS_URL")
	if url != "" {
		st, err := adminsession.NewRedisStoreFromURL(ctx, url, ttl)
		if err != nil {
			return nil, err
		}
		return adminsession.New(st, ttl), nil
	}
	if config.IsProduction() {
		return nil, errors.New("stack: REDIS_URL is required in production")
	}
	return adminsession.New(nil, ttl), nil
}

// OpenReplayGuard 按环境打开 TOTP 重放防护：
// 多实例必须用 Redis，否则同一验证码可在不同实例重复使用。
func OpenReplayGuard(ctx context.Context) (adminapi.ReplayGuard, error) {
	url := os.Getenv("REDIS_URL")
	if url != "" {
		return adminapi.NewRedisReplayGuardFromURL(ctx, url, 5*time.Minute)
	}
	if config.IsProduction() {
		return nil, errors.New("stack: REDIS_URL is required in production for TOTP replay guard")
	}
	return adminapi.NewMemoryReplayGuard(), nil
}

// OpenLimiter 按环境打开限流器：有 REDIS_URL 用 Redis（跨实例），
// 生产缺配置直接失败，开发环境退回内存。
func OpenLimiter(ctx context.Context) (ratelimit.Limiter, error) {
	url := os.Getenv("REDIS_URL")
	if url != "" {
		return ratelimit.NewRedisFromURL(ctx, url, "er:rl:")
	}
	if config.IsProduction() {
		return nil, errors.New("stack: REDIS_URL is required in production for rate limiting")
	}
	return ratelimit.NewMemory(), nil
}
