// Package adminhttp 提供管理后台 HTTP 处理器。cmd/admin 只做装配。
package adminhttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/ratelimit"
)

// App 是后台 HTTP 依赖。
type App struct {
	Limiter  ratelimit.Limiter
	Service  *adminapi.Service
	Sessions *adminsession.Service
}

// writeErr 写错误信封。detail 只进日志，不回给调用方。
func writeErr(w http.ResponseWriter, r *http.Request, err error) { httpx.Err(w, r, err) }

// ActorFrom 从服务端会话表解析操作者身份。
// 角色只来自服务端会话，不接受客户端自报的任何身份数据。
func (a *App) ActorFrom(r *http.Request) (adminapi.Actor, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return adminapi.Actor{}, false
	}
	sess, ok := a.Sessions.Verify(r.Context(), strings.TrimPrefix(h, "Bearer "))
	if !ok {
		return adminapi.Actor{}, false
	}
	return adminapi.Actor{Username: sess.Username, Role: sess.Role, IP: clientIP(r)}, true
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	return r.RemoteAddr
}

// Routes 返回后台路由。所有响应只走 httpx.OK / OKPage / Err。
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TOTP     string `json:"totp"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.AuthInvalidCredential, "bad body"))
			return
		}
		if a.Limiter != nil {
			if res, _ := a.Limiter.Allow(r.Context(), "adminlogin:"+req.Username, 5, 10*time.Minute); !res.Allowed {
				writeErr(w, r, errcode.New(errcode.AuthTooManyAttempts, "too many attempts"))
				return
			}
		}
		u, err := a.Service.Authenticate(r.Context(), req.Username, req.Password, req.TOTP)
		a.Service.AuditLogin(r.Context(), req.Username, clientIP(r), err == nil)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		tok, sess, err := a.Sessions.Issue(r.Context(), u.ID, u.Username, u.Role)
		if err != nil || tok == "" {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "session issue failed"))
			return
		}
		httpx.OK(w, r, contract.AdminSession{
			Token:     tok,
			Username:  sess.Username,
			Role:      sess.Role,
			ExpiresAt: sess.ExpiresAt,
		})
	})

	mux.HandleFunc("/admin/v1/logout", func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			if err := a.Sessions.Revoke(r.Context(), strings.TrimPrefix(h, "Bearer ")); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "revoke failed"))
				return
			}
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/stats/overview", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		stats, err := a.Service.Overview(r.Context(), act)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, stats)
	})

	mux.HandleFunc("/admin/v1/users", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListUsers(r.Context(), act)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OKPage(w, r, httpx.Page[contract.UserRow]{Items: list, Total: len(list)})
	})

	mux.HandleFunc("/admin/v1/users/ban", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req struct {
			AccountID string `json:"account_id"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if err := a.Service.BanUser(r.Context(), act, req.AccountID, req.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListDevices(r.Context(), act)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OKPage(w, r, httpx.Page[contract.DeviceRow]{Items: list, Total: len(list)})
	})

	mux.HandleFunc("/admin/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListSessions(r.Context(), act, r.URL.Query().Get("state"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OKPage(w, r, httpx.Page[contract.SessionRow]{Items: list, Total: len(list)})
	})

	mux.HandleFunc("/admin/v1/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req struct {
			SessionID string `json:"session_id"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if err := a.Service.KillSession(r.Context(), act, req.SessionID, req.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/config", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			vals, err := a.Service.GetConfig(r.Context(), act)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			fields, err := a.Service.ConfigFields(r.Context(), act)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			view := make([]contract.ConfigField, 0, len(fields))
			for _, f := range fields {
				view = append(view, contract.ConfigField{
					Key: f.Key, Kind: string(f.Kind), Default: f.Default, Min: f.Min, Max: f.Max, Help: f.Help,
				})
			}
			httpx.OK(w, r, contract.ConfigView{Config: vals, Fields: view})
		case http.MethodPut:
			var req struct {
				Values map[string]string `json:"values"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			vals, err := a.Service.PutConfig(r.Context(), act, req.Values)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, contract.ConfigPutResult{Config: vals, Audited: true})
		default:
			writeErr(w, r, errcode.New(errcode.AdminForbidden, "method not allowed"))
		}
	})

	mux.HandleFunc("/admin/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		logs, err := a.Service.Audit(r.Context(), act, 100)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		rows := make([]contract.AuditRow, 0, len(logs))
		for _, l := range logs {
			rows = append(rows, contract.AuditRow{
				ID: l.ID, Actor: l.Actor, Role: l.Role, Action: l.Action, Object: l.Object,
				Before: l.Before, After: l.After, IP: l.IP, Result: l.Result, TS: l.TS,
			})
		}
		httpx.OKPage(w, r, httpx.Page[contract.AuditRow]{Items: rows, Total: len(rows)})
	})

	return httpx.Middleware(httpx.JSONMux(mux))
}
