package stack

import (
	"context"
	"embed"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminhttp"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/gateway"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/relaymgr"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// LoadMigration 读取内嵌的建表脚本。
func LoadMigration() (string, error) {
	b, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// NewWithStoreAndSessions 用给定存储与会话后端组装 handler。
// NewWithStoreAndSessions 组装 handler；hub 非空时网关会把建会话事件推给信令。
func NewWithStoreAndSessions(st store.Store, sessions *adminsession.Service, limiter ratelimit.Limiter, replay adminapi.ReplayGuard) (http.Handler, error) {
	// 二次验证开关先判定：生产环境配了这个变量必须直接失败，不能静默降级。
	mfaOff, err := adminMFADisabled()
	if err != nil {
		return nil, err
	}
	secret, err := config.Secret()
	if err != nil {
		return nil, err
	}
	authSvc, err := auth.New(st, devSMS(), secret)
	if err != nil {
		return nil, err
	}
	sessSvc := session.New(st)
	gw := &gateway.App{
		Notifier: NotifierFromEnv(),
		Limiter:  limiter,
		Auth:     authSvc,
		Device:   device.New(st, secret),
		Session:  sessSvc,
		Relay:    relaymgr.New(secret, time.Hour),
		Store:    st,
	}
	adminSvc := adminapi.NewWithReplay(st, sessSvc, replay)
	if mfaOff {
		adminSvc.SetDevSkipTOTP(true)
	}
	adm := &adminhttp.App{
		Limiter:  limiter,
		Service:  adminSvc,
		Sessions: sessions,
	}

	mux := http.NewServeMux()
	mux.Handle("/admin/", adm.Routes())
	mux.Handle("/", gw.Routes())
	return mux, nil
}

// adminMFADisabled 读取开发用二次验证开关 ER_ADMIN_TOTP_OFF。
// 生产环境配了它直接报错：安全要求不允许被环境变量静默关掉。
func adminMFADisabled() (bool, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ER_ADMIN_TOTP_OFF")))
	on := v == "1" || v == "true" || v == "yes"
	if on && config.IsProduction() {
		return false, errors.New("stack: ER_ADMIN_TOTP_OFF is forbidden in production")
	}
	return on, nil
}

// SeedDevAdmin 写入开发用管理员（幂等）。
//
// 生产环境禁止调用：否则等于内置一个已知口令 + 已知 TOTP 密钥的超管。
// 生产开户由运维通过独立流程完成（见 docs/impl/runbook.md）。
func SeedDevAdmin(ctx context.Context, st store.Store) error {
	if config.IsProduction() {
		return errors.New("stack: refusing to seed development admin in production")
	}
	return st.PutAdmin(ctx, &store.AdminUser{
		Username:     "root",
		PasswordHash: adminapi.HashPassword("dev-pass"),
		TOTPSecret:   DevTOTPSecret,
		Role:         adminapi.RoleSuper,
		Status:       "active",
	})
}

// NewDev 内存环境（本地联调、集成测试）。
func NewDev() (*store.Memory, http.Handler, error) {
	mem := store.NewMemory()
	if err := SeedDevAdmin(context.Background(), mem); err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(mem, adminsession.New(nil, 8*time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	return mem, h, err
}

// NewPostgres 生产形态：PG 存储 + 自动迁移 + 种子管理员。
func NewPostgres(ctx context.Context, dsn string) (store.Store, http.Handler, error) {
	pg, err := store.NewPG(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	ddl, err := LoadMigration()
	if err != nil {
		return nil, nil, err
	}
	if err := pg.Migrate(ctx, ddl); err != nil {
		return nil, nil, err
	}
	if err := SeedDevAdmin(ctx, pg); err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(pg, adminsession.New(nil, 8*time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	if err != nil {
		return nil, nil, err
	}
	return pg, h, nil
}

// NewPostgresWithRedis 生产形态：PG 存储 + Redis 会话（支持多实例）。
func NewPostgresWithRedis(ctx context.Context, dsn, redisURL string) (store.Store, http.Handler, error) {
	pg, err := store.NewPG(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	ddl, err := LoadMigration()
	if err != nil {
		return nil, nil, err
	}
	if err := pg.Migrate(ctx, ddl); err != nil {
		return nil, nil, err
	}
	if err := SeedDevAdmin(ctx, pg); err != nil {
		return nil, nil, err
	}
	sessStore, err := adminsession.NewRedisStoreFromURL(ctx, redisURL, 8*time.Hour)
	if err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(pg, adminsession.New(sessStore, 8*time.Hour), ratelimit.NewMemory(), adminapi.NewMemoryReplayGuard())
	if err != nil {
		return nil, nil, err
	}
	return pg, h, nil
}
