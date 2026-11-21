package gateway

import (
	"net/http"
	"time"

	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
)

// 邮箱登录：把原型里的「邮箱登录 V1」占位变成真接口。
//
// 与手机号登录同构：先取码（60 秒一条），再用码换令牌；
// 邮箱不存在时自动建号，令牌与短信登录由同一套签名逻辑签发，
// 因此下游依赖（设备绑定、建连、文件）不需要区分登录方式。
func (a *App) emailSendCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "bad body"))
		return
	}
	normalized, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if a.Limiter != nil {
		// 60 秒一条：键用归一化后的邮箱，避免 Alice@ 与 alice@ 各发一条。
		if res, _ := a.Limiter.Allow(ctx, "email:code:"+normalized, 1, time.Minute); !res.Allowed {
			WriteErr(w, errcode.New(errcode.AuthSmsRateLimited, "email code too frequent"))
			return
		}
		if res, _ := a.Limiter.Allow(ctx, "email:code:ip:"+clientIP(r), 30, time.Hour); !res.Allowed {
			WriteErr(w, errcode.New(errcode.AuthSmsRateLimited, "email code ip quota"))
			return
		}
	}
	if err := a.Auth.SendEmailCode(normalized); err != nil {
		WriteErr(w, err)
		return
	}
	httpx.OK(w, r, map[string]any{"ok": true, "email": normalized})
}

func (a *App) emailLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "bad body"))
		return
	}
	normalized, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if a.Limiter != nil {
		if res, _ := a.Limiter.Allow(ctx, "login:email:"+normalized, 10, time.Hour); !res.Allowed {
			WriteErr(w, errcode.New(errcode.AuthTooManyAttempts, "email login attempts exceeded"))
			return
		}
	}
	tok, acc, err := a.Auth.LoginEmail(ctx, normalized, req.Code)
	if err != nil {
		WriteErr(w, err)
		return
	}
	httpx.OK(w, r, map[string]any{"token": tok, "account_id": acc.ID, "email": normalized})
}
