// Package adminhttp 提供管理后台 HTTP 处理器。cmd/admin 只做装配。
package adminhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/adminsession"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/store"
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
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

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
			Token:       tok,
			Username:    sess.Username,
			Role:        sess.Role,
			Permissions: adminapi.Permissions(sess.Role),
			ExpiresAt:   sess.ExpiresAt,
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
		// range=24h|7d|30d 控制质量统计窗口；缺省 24h。
		window := 24 * time.Hour
		switch r.URL.Query().Get("range") {
		case "7d":
			window = 7 * 24 * time.Hour
		case "30d":
			window = 30 * 24 * time.Hour
		}
		stats, err := a.Service.OverviewSince(r.Context(), act, time.Now().UTC().Add(-window))
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
		list, err := a.Service.ListUsers(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountUsers(r.Context(), act, parsePage(r))
		httpx.OKPage(w, r, httpx.Page[contract.UserRow]{Items: list, Total: total})
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
		list, err := a.Service.ListDevices(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountDevices(r.Context(), act, parsePage(r))
		httpx.OKPage(w, r, httpx.Page[contract.DeviceRow]{Items: list, Total: total})
	})

	// ADM-12/13：设备管理动作，原因必填并写审计。
	deviceAction := func(action func(ctx context.Context, a adminapi.Actor, deviceID, reason string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			act, ok := a.ActorFrom(r)
			if !ok {
				writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
				return
			}
			var req struct {
				DeviceID string `json:"device_id"`
				Reason   string `json:"reason"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			if err := action(r.Context(), act, req.DeviceID, req.Reason); err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, contract.OkResult{OK: true})
		}
	}
	mux.HandleFunc("/admin/v1/devices/force-offline", deviceAction(a.Service.ForceOffline))
	mux.HandleFunc("/admin/v1/devices/unbind", deviceAction(a.Service.UnbindDevice))
	mux.HandleFunc("/admin/v1/devices/flag-risk", deviceAction(a.Service.FlagDeviceRisk))
	mux.HandleFunc("/admin/v1/devices/clear-risk", deviceAction(a.Service.ClearDeviceRisk))

	mux.HandleFunc("/admin/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListSessions(r.Context(), act, r.URL.Query().Get("state"), parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountSessions(r.Context(), act, r.URL.Query().Get("state"), parsePage(r))
		httpx.OKPage(w, r, httpx.Page[contract.SessionRow]{Items: list, Total: total})
	})

	mux.HandleFunc("/admin/v1/risk/blacklist", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		if r.Method == http.MethodPost {
			var req contract.BlacklistAddRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			row, err := a.Service.AddBlacklist(r.Context(), act, req)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, row)
			return
		}
		list, err := a.Service.ListBlacklist(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountBlacklist(r.Context(), act)
		httpx.OKPage(w, r, httpx.Page[contract.BlacklistRow]{Items: list, Total: total})
	})

	mux.HandleFunc("/admin/v1/risk/blacklist/remove", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req contract.BlacklistRemoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if err := a.Service.RemoveBlacklist(r.Context(), act, req.ID, req.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/risk/alerts", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListAlerts(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountAlerts(r.Context(), act)
		httpx.OKPage(w, r, httpx.Page[contract.RiskAlertRow]{Items: list, Total: total})
	})

	mux.HandleFunc("/admin/v1/versions", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		if r.Method == http.MethodPost {
			var req contract.VersionPublishRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			row, err := a.Service.PublishVersion(r.Context(), act, req)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, row)
			return
		}
		list, err := a.Service.ListVersions(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountVersions(r.Context(), act)
		httpx.OKPage(w, r, httpx.Page[contract.ClientVersionRow]{Items: list, Total: total})
	})

	mux.HandleFunc("/admin/v1/versions/retire", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req contract.VersionRetireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if err := a.Service.RetireVersion(r.Context(), act, req.VersionID, req.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/admins", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		list, err := a.Service.ListAdmins(r.Context(), act, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		total, _ := a.Service.CountAdmins(r.Context(), act)
		httpx.OKPage(w, r, httpx.Page[contract.AdminRow]{Items: list, Total: total})
	})

	mux.HandleFunc("/admin/v1/admins/update", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req contract.AdminUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if err := a.Service.UpdateAdmin(r.Context(), act, req.AdminID, req.Role, req.Status, req.Reason); err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/admin/v1/devices/quality", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		res, err := a.Service.DeviceQuality(r.Context(), act, r.URL.Query().Get("device_id"), days)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/sessions/kill-request", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req contract.KillRequestCreate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		row, err := a.Service.RequestKill(r.Context(), act, req.SessionID, req.Reason)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, row)
	})

	mux.HandleFunc("/admin/v1/sessions/kill-confirm", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req contract.KillConfirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		res, err := a.Service.ConfirmKill(r.Context(), act, req.RequestID)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/sessions/kill-requests", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		status := r.URL.Query().Get("status")
		list, err := a.Service.ListKillRequests(r.Context(), act, status, parsePage(r))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OKPage(w, r, httpx.Page[contract.KillRequestRow]{Items: list, Total: len(list)})
	})

	mux.HandleFunc("/admin/v1/sessions/detail", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		res, err := a.Service.SessionDetail(r.Context(), act, r.URL.Query().Get("session_id"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/sessions/quality", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		res, err := a.Service.SessionQuality(r.Context(), act, r.URL.Query().Get("session_id"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
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
		result, err := a.Service.KillSession(r.Context(), act, req.SessionID, req.Reason)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, result)
	})

	mux.HandleFunc("/admin/v1/config", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			platform := r.URL.Query().Get("platform")
			version := r.URL.Query().Get("version")
			vals, err := a.Service.GetConfigScope(r.Context(), act, platform, version)
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
					Key: f.Key, Kind: string(f.Kind), Default: f.Default, Min: f.Min, Max: f.Max, Help: f.Help, Group: f.Group,
				})
			}
			httpx.OK(w, r, contract.ConfigView{Config: vals, Fields: view, Platform: platform, Version: version})
		case http.MethodPut:
			var req contract.ConfigPutRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			vals, err := a.Service.PutConfigScope(r.Context(), act, req.Platform, req.Version, req.Values)
			if err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, contract.ConfigPutResult{Config: vals, Audited: true})
		default:
			writeErr(w, r, errcode.New(errcode.AdminForbidden, "method not allowed"))
		}
	})

	mux.HandleFunc("/admin/v1/config/history", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		res, err := a.Service.ConfigHistory(r.Context(), act, limit)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/config/rollback", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		cfg, err := a.Service.RollbackConfig(r.Context(), act, req.Key, req.Value)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, contract.ConfigPutResult{Config: cfg, Audited: true})
	})

	userAction := func(action func(ctx context.Context, a adminapi.Actor, accountID, reason string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			act, ok := a.ActorFrom(r)
			if !ok {
				writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
				return
			}
			var req contract.UserActionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			if err := action(r.Context(), act, req.AccountID, req.Reason); err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, contract.OkResult{OK: true})
		}
	}
	mux.HandleFunc("/admin/v1/users/export", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		res, err := a.Service.ExportUsers(r.Context(), act, parsePage(r), limit)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/users/unban", userAction(a.Service.UnbanUser))
	mux.HandleFunc("/admin/v1/users/kick", userAction(a.Service.KickUser))

	mux.HandleFunc("/admin/v1/users/sessions", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := a.Service.ListUserSessions(r.Context(), act, r.URL.Query().Get("account_id"), limit)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OKPage(w, r, httpx.Page[contract.SessionRow]{Items: list, Total: len(list)})
	})

	mux.HandleFunc("/admin/v1/users/note", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		if r.Method == http.MethodPost {
			var req contract.AccountNoteSet
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, r, errcode.New(errcode.SrvInternal, "bad body"))
				return
			}
			if err := a.Service.SetUserNote(r.Context(), act, req.AccountID, req.Note); err != nil {
				writeErr(w, r, err)
				return
			}
			httpx.OK(w, r, contract.OkResult{OK: true})
			return
		}
		row, err := a.Service.GetUserNote(r.Context(), act, r.URL.Query().Get("account_id"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, row)
	})

	mux.HandleFunc("/admin/v1/stats/trends", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		res, err := a.Service.Trends(r.Context(), act, r.URL.Query().Get("range"), r.URL.Query().Get("bucket"))
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/audit/export", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		res, err := a.Service.ExportAudit(r.Context(), act, parsePage(r), limit)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/audit/actions", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		res, err := a.Service.AuditActions(r.Context(), act)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.OK(w, r, res)
	})

	mux.HandleFunc("/admin/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		act, ok := a.ActorFrom(r)
		if !ok {
			writeErr(w, r, errcode.New(errcode.AdminSessionExpired, "unauthorized"))
			return
		}
		logs, err := a.Service.Audit(r.Context(), act, parsePage(r))
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
		total, _ := a.Service.CountAudit(r.Context(), act, parsePage(r))
		httpx.OKPage(w, r, httpx.Page[contract.AuditRow]{Items: rows, Total: total})
	})

	return securityHeaders(httpx.Middleware(httpx.MethodGuard(routeSpecs, httpx.JSONMux(mux))))
}

// parsePage 解析后台列表的 limit/offset/q/sort/desc；上限 500，防止无限拉取。
// parseBoolPtr 解析 1/0/true/false；空串或非法值返回 nil（不限）。
func parseBoolPtr(v string) *bool {
	switch strings.TrimSpace(v) {
	case "1", "true":
		b := true
		return &b
	case "0", "false":
		b := false
		return &b
	default:
		return nil
	}
}

// parseTime 解析 RFC3339；空串或格式错误都视为"不限"，避免坏参数把整页清空。
func parseTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parsePage(r *http.Request) store.Page {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	// online=1/0（或 true/false）过滤设备在线状态；缺省不过滤。
	var online *bool
	switch strings.TrimSpace(q.Get("online")) {
	case "1", "true":
		v := true
		online = &v
	case "0", "false":
		v := false
		online = &v
	}
	return store.Page{
		Limit:     limit,
		Offset:    offset,
		Query:     strings.TrimSpace(q.Get("q")),
		Sort:      strings.TrimSpace(q.Get("sort")),
		Desc:      q.Get("desc") == "1" || q.Get("desc") == "true",
		Status:    strings.TrimSpace(q.Get("status")),
		Platform:  strings.TrimSpace(q.Get("platform")),
		Online:    online,
		Actor:     strings.TrimSpace(q.Get("actor")),
		Object:    strings.TrimSpace(q.Get("object")),
		AccountID: strings.TrimSpace(q.Get("account_id")),
		DeviceID:  strings.TrimSpace(q.Get("device_id")),
		CloseKind: strings.TrimSpace(q.Get("close_kind")),
		Link:      strings.TrimSpace(q.Get("link")),
		Abnormal:  parseBoolPtr(q.Get("abnormal")),
		From:      parseTime(q.Get("from")),
		To:        parseTime(q.Get("to")),
		Action:    strings.TrimSpace(q.Get("action")),
		Result:    strings.TrimSpace(q.Get("result")),
	}
}
