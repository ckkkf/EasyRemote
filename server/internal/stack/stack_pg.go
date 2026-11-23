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
func NewWithStoreAndSessions(st store.Store, sessions *adminsession.Service, limiter ratelimit.Limiter, replay adminapi.ReplayGuard, notifier gateway.ConsentNotifier, presence gateway.Presence, closer adminapi.SessionCloser) (http.Handler, error) {
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
	// 僵尸 active 清理：参与者双方在线租约都有效才保留；一方离线则关闭并传播。
	if presence != nil && closer != nil {
		jan := &session.Janitor{Sessions: sessSvc, Presence: presence, Closer: closer, Interval: 30 * time.Second}
		go jan.Run(context.Background())
	}
	var epochs gateway.TicketEpochs
	if e, ok := notifier.(gateway.TicketEpochs); ok {
		epochs = e
	}
	gw := &gateway.App{
		Notifier:     notifier,
		Presence:     presence,
		Closer:       closer,
		TicketEpochs: epochs,
		Limiter:      limiter,
		Auth:         authSvc,
		Device:       device.New(st, secret),
		Session:      sessSvc,
		Relay:        relaymgr.New(secret, time.Hour),
		Store:        st,
	}
	adminSvc := adminapi.NewWithReplay(st, sessSvc, replay).
		SetSessionCloser(closer).
		SetPresence(presence)
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

// seedDevAdminIfNotProduction 只在非生产环境写入开发管理员。
// 生产启动绝不创建已知账号（A-07）；开户走独立运维流程。
func seedDevAdminIfNotProduction(ctx context.Context, st store.Store) error {
	if config.IsProduction() {
		return nil
	}
	return SeedDevAdmin(ctx, st)
}

// devLimiter 返回开发限流器：ER_RATELIMIT_OFF=1 且非生产时无限流，否则内存限流。
//
// 装配层原来直接写死 ratelimit.NewMemory()，导致开关只改 OpenLimiter 时对
// cmd/stack 完全无效（限流照样锁人）。所有开发装配入口必须走这里。
// 生产环境忽略该开关（ValidateDeployment 另有硬拒绝，双保险）。
func devLimiter() ratelimit.Limiter {
	if truthyEnv(os.Getenv("ER_RATELIMIT_OFF")) && !config.IsProduction() {
		return ratelimit.NewUnlimited()
	}
	return ratelimit.NewMemory()
}

// NewDev 内存环境（本地联调、集成测试）。
func NewDev() (*store.Memory, http.Handler, error) {
	mem := store.NewMemory()
	if err := SeedDevAdmin(context.Background(), mem); err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(mem, adminsession.New(nil, 8*time.Hour), devLimiter(), adminapi.NewMemoryReplayGuard(), NotifierFromEnv(), nil, NotifierFromEnv())
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
	if err := seedDevAdminIfNotProduction(ctx, pg); err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(pg, adminsession.New(nil, 8*time.Hour), devLimiter(), adminapi.NewMemoryReplayGuard(), nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	return pg, h, nil
}

// NewPostgresWithRedis 生产形态：PG 存储 + Redis 会话（支持多实例）。
func NewPostgresWithRedis(ctx context.Context, dsn, redisURL string, notifier gateway.ConsentNotifier, presence gateway.Presence, closer adminapi.SessionCloser) (store.Store, http.Handler, error) {
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
	if err := seedDevAdminIfNotProduction(ctx, pg); err != nil {
		return nil, nil, err
	}
	sessStore, err := adminsession.NewRedisStoreFromURL(ctx, redisURL, 8*time.Hour)
	if err != nil {
		return nil, nil, err
	}
	h, err := NewWithStoreAndSessions(pg, adminsession.New(sessStore, 8*time.Hour), devLimiter(), adminapi.NewMemoryReplayGuard(), notifier, presence, closer)
	if err != nil {
		return nil, nil, err
	}
	return pg, h, nil
}
