package stack

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminhttp"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/gateway"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/relaymgr"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// BuildGateway 组装客户端 API handler。cmd/gateway 与 stack 共用。
func BuildGateway(st store.Store, secret []byte, limiter ratelimit.Limiter, sms auth.SMSProvider) (http.Handler, error) {
	if sms == nil {
		return nil, errors.New("stack: SMS provider is required")
	}
	authSvc, err := auth.New(st, sms, secret)
	if err != nil {
		return nil, err
	}
	app := &gateway.App{
		Limiter: limiter,
		Auth:    authSvc,
		Device:  device.New(st, secret),
		Session: session.New(st),
		Relay:   relaymgr.New(secret, time.Hour),
		Store:   st,
		Config:  appconfig.New(st),
		// 落盘目录可用 ER_FILES_DIR 覆盖（容器里通常挂卷）；默认 os.TempDir()/easyremote-files。
		FilesDir: os.Getenv("ER_FILES_DIR"),
	}
	return app.Routes(), nil
}

// BuildAdmin 组装管理后台 API handler。
func BuildAdmin(st store.Store, sessions *adminsession.Service, limiter ratelimit.Limiter, replay adminapi.ReplayGuard) (http.Handler, error) {
	return (&adminhttp.App{
		Limiter: limiter,
		Service: adminapi.NewWithReplay(st, session.New(st), replay).
			SetConfig(appconfig.New(st)),
		Sessions: sessions,
	}).Routes(), nil
}
