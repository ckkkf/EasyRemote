// Package gateway 提供客户端 HTTP 处理器。cmd/gateway 只做装配。
package gateway

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/ratelimit"
	"github.com/easyremote/server/internal/relaymgr"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// App 是网关依赖集合。
// ConsentNotifier 把「有人要连你」推给信令层。
//
// 可选：不注入时行为与之前一致（只落库），端到端会卡在等待授权。
type ConsentNotifier interface {
	NotifyConsentRequired(sessionID, from, to string) error
}

type App struct {
	Notifier ConsentNotifier
	Limiter  ratelimit.Limiter
	Auth     *auth.Service
	Device   *device.Service
	Session  *session.Service
	Relay    *relaymgr.Service
	Store    store.Store
	// Config 是运行时配置中心（P1-1）。nil 时全部使用默认值。
	Config *appconfig.Service
	// FilesDir 是文件落盘根目录；空则用 os.TempDir()/easyremote-files。
	FilesDir string
	// Tickets 是下载票据表（惰性初始化，见 files.go）。
	Tickets  *ticketStore
	ticketMu sync.Mutex
}

// WriteErr 转发到统一实现：detail 只进日志，不回给调用方。
func WriteErr(w http.ResponseWriter, err error) { httpx.Err(w, nil, err) }

// requireDevice 校验请求方持有目标设备的凭证。
// 这是「只有被控机自己才能轮换验证码 / 授权 / 领票据」的强制点。
func (a *App) requireDevice(ctx context.Context, r *http.Request, deviceID string) error {
	token := r.Header.Get("X-Device-Token")
	if !a.Device.VerifyDeviceToken(ctx, deviceID, token) {
		return errcode.New(errcode.AdminForbidden, "device credential required")
	}
	return nil
}

func (a *App) deviceByCode(ctx context.Context, code string) (*store.Device, error) {
	return a.Store.GetDeviceByCode(ctx, strings.ReplaceAll(code, " ", ""))
}

// clientIP 取真实来源 IP，用于限流键。
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.Split(ip, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) account(ctx context.Context, r *http.Request) (*store.Account, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, errcode.New(errcode.AuthTokenExpired, "missing bearer token")
	}
	return a.Auth.VerifyToken(ctx, strings.TrimPrefix(h, "Bearer "))
}

// Routes 返回网关的全部路由。
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, r, struct {
			Service string `json:"service"`
		}{Service: "gateway"})
	})

	mux.HandleFunc("/v1/auth/sms", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			Phone string `json:"phone"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "bad body"))
			return
		}
		if a.Limiter != nil {
			if res, _ := a.Limiter.Allow(ctx, "sms:phone:"+req.Phone, 1, time.Minute); !res.Allowed {
				WriteErr(w, errcode.New(errcode.AuthSmsRateLimited, "too frequent"))
				return
			}
			if res, _ := a.Limiter.Allow(ctx, "sms:ip:"+clientIP(r), 10, time.Hour); !res.Allowed {
				WriteErr(w, errcode.New(errcode.AuthSmsRateLimited, "ip quota"))
				return
			}
		}
		if err := a.Auth.SendCode(req.Phone); err != nil {
			WriteErr(w, err)
			return
		}
		httpx.OK(w, r, map[string]any{"ok": true})
	})

	mux.HandleFunc("/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			Phone string `json:"phone"`
			Code  string `json:"code"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "bad body"))
			return
		}
		if a.Limiter != nil {
			if res, _ := a.Limiter.Allow(ctx, "login:phone:"+req.Phone, 10, time.Hour); !res.Allowed {
				WriteErr(w, errcode.New(errcode.AuthTooManyAttempts, "login attempts exceeded"))
				return
			}
		}
		tok, acc, err := a.Auth.Login(ctx, req.Phone, req.Code)
		if err != nil {
			WriteErr(w, err)
			return
		}
		httpx.OK(w, r, map[string]any{"token": tok, "account_id": acc.ID})
	})

	mux.HandleFunc("/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		list, err := a.Device.List(ctx, acc.ID)
		if err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
			return
		}
		out := make([]map[string]any, 0, len(list))
		for _, d := range list {
			out = append(out, map[string]any{
				"device_code": device.FormatCode(d.DeviceCode),
				"raw_code":    d.DeviceCode,
				"platform":    d.Platform,
				"online":      d.Online,
				"last_seen":   d.LastSeenAt,
			})
		}
		httpx.OK(w, r, map[string]any{"devices": out})
	})

	mux.HandleFunc("/v1/devices/bind", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		var req struct {
			Platform string `json:"platform"`
		}
		_ = httpx.Decode(w, r, &req)
		if req.Platform == "" {
			req.Platform = "windows"
		}
		d, deviceToken, err := a.Device.Bind(ctx, acc.ID, req.Platform)
		if err != nil {
			WriteErr(w, err)
			return
		}
		// device_token 只在此处返回一次，客户端须安全保存。
		httpx.OK(w, r, map[string]any{
			"device_code":  device.FormatCode(d.DeviceCode),
			"raw_code":     d.DeviceCode,
			"device_id":    d.ID,
			"device_token": deviceToken,
		})
	})

	mux.HandleFunc("/v1/devices/verify-code", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			DeviceCode string `json:"device_code"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		// 只有被控设备本机能轮换自己的验证码
		dev, err := a.deviceByCode(ctx, req.DeviceCode)
		if err != nil {
			WriteErr(w, errcode.New(errcode.ConnDeviceNotFound, "device not found"))
			return
		}
		if err := a.requireDevice(ctx, r, dev.ID); err != nil {
			WriteErr(w, err)
			return
		}
		code, exp, err := a.Device.RotateCode(ctx, dev.DeviceCode)
		if err != nil {
			WriteErr(w, err)
			return
		}
		httpx.OK(w, r, map[string]any{"code": code, "expires_at": exp})
	})

	// 被控端设置「临时密码」：长期有效、校验不消费、可反复使用。
	// 明文只留在被控端本地（可查看/复制），服务端只存 HMAC。
	mux.HandleFunc("/v1/devices/password", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		var req struct {
			DeviceID string `json:"device_id"`
			Password string `json:"password"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		d, err := a.Store.GetDevice(ctx, req.DeviceID)
		if err != nil || d.AccountID != acc.ID {
			WriteErr(w, errcode.New(errcode.ConnDeviceNotFound, "device not found"))
			return
		}
		if err := a.Device.SetPassword(ctx, d.ID, req.Password); err != nil {
			WriteErr(w, err)
			return
		}
		// 新路由用具体类型，不再往存量的无类型响应体里加（gate 禁止模式）
		httpx.OK(w, r, struct {
			OK bool `json:"ok"`
		}{OK: true})
	})

	mux.HandleFunc("/v1/connect/request", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		var req struct {
			DeviceCode string `json:"device_code"`
			Captcha    string `json:"captcha"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		if a.Limiter != nil {
			if res, _ := a.Limiter.Allow(ctx, "connect:"+acc.ID, 30, time.Minute); !res.Allowed {
				WriteErr(w, errcode.New(errcode.ConnRateLimited, "connect rate limited"))
				return
			}
		}
		target, err := a.Store.GetDeviceByCode(ctx, strings.ReplaceAll(req.DeviceCode, " ", ""))
		if err != nil {
			WriteErr(w, errcode.New(errcode.ConnDeviceNotFound, "device not found or not accepting"))
			return
		}
		// 主控设备必须由请求头里的设备凭证确定。
		// 早期用 devices[0] 代替，导致同账号绑了多台设备时
		// controller_id 指向一台没参与本次请求的设备，
		// 后续 /v1/relay/ticket 用真实设备凭证校验时必然 700005。
		controllerID, err := a.deviceFromCredential(ctx, r, acc.ID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		if controllerID == target.ID {
			WriteErr(w, errcode.New(errcode.ConnSelfConnect, "self connect"))
			return
		}
		trusted := a.Device.HasTrust(ctx, controllerID, target.ID)
		if !trusted {
			if req.Captcha == "" {
				WriteErr(w, errcode.New(errcode.ConnCaptchaExpired, "captcha required"))
				return
			}
			// 先按「临时密码」校验：长期有效、校验不消费（被控端可查看/复制同一串密码反复用）。
			// 不匹配再回退到一次性验证码，保证旧客户端与旧流程不受影响。
			if !a.Device.CheckPassword(ctx, target.DeviceCode, req.Captcha) {
				if err := a.Device.VerifyCode(ctx, target.DeviceCode, req.Captcha); err != nil {
					WriteErr(w, err)
					return
				}
			}
		}
		sess, err := a.Session.Request(ctx, controllerID, target.ID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 通知被控端有新的授权请求。
		// 不通知的话，被控端的信令长连接永远等不到 CONSENT_REQUIRED，
		// 端到端会卡在「主控已发起、被控毫无反应」。
		if a.Notifier != nil && sess.State == session.StateRequested {
			_ = a.Notifier.NotifyConsentRequired(sess.ID, controllerID, target.ID)
		}
		httpx.OK(w, r, map[string]any{"session_id": sess.ID, "state": sess.State, "trusted": trusted})
	})

	mux.HandleFunc("/v1/connect/consent", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			SessionID string `json:"session_id"`
			Accepted  bool   `json:"accepted"`
			Reason    string `json:"reason"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		// 只有被控端自己能对会话做授权决策
		pending, err := a.Session.Get(ctx, req.SessionID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		if err := a.requireDevice(ctx, r, pending.TargetID); err != nil {
			WriteErr(w, err)
			return
		}
		sess, err := a.Session.Consent(ctx, req.SessionID, req.Accepted, req.Reason)
		if err != nil {
			WriteErr(w, err)
			return
		}
		httpx.OK(w, r, map[string]any{"session_id": sess.ID, "state": sess.State, "auth_expire_at": sess.AuthExpireAt})
	})

	mux.HandleFunc("/v1/connect/status", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sess, err := a.Session.Get(ctx, r.URL.Query().Get("session_id"))
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 会话状态同样只对会话双方可见
		if a.requireDevice(ctx, r, sess.ControllerID) != nil && a.requireDevice(ctx, r, sess.TargetID) != nil {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		httpx.OK(w, r, map[string]any{"session_id": sess.ID, "state": sess.State, "reason": sess.Reason})
	})

	// 设备登记长期身份公钥（Ed25519 hex）。
	// 没有它，端到端加密的握手验签就无从谈起（只能 TOFU）。
	mux.HandleFunc("/v1/devices/identity", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		var req struct {
			DeviceID string `json:"device_id"`
			PubKey   string `json:"pubkey"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "bad body"))
			return
		}
		if len(req.PubKey) != 64 || !isHex(req.PubKey) {
			WriteErr(w, errcode.New(errcode.AuthInvalidCredential, "pubkey must be 32 bytes hex"))
			return
		}
		dev, err := a.Store.GetDevice(ctx, req.DeviceID)
		if err != nil || dev.AccountID != acc.ID {
			WriteErr(w, errcode.New(errcode.ConnDeviceNotFound, "device not found"))
			return
		}
		if err := a.requireDevice(ctx, r, dev.ID); err != nil {
			WriteErr(w, err)
			return
		}
		if err := a.Store.UpdateDeviceIdentity(ctx, dev.ID, req.PubKey); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
			return
		}
		httpx.OK(w, r, map[string]any{"ok": true})
	})

	// 下发会话双方的身份公钥：只有会话两端能读，且只对活跃会话有效。
	mux.HandleFunc("/v1/connect/keys", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		sess, err := a.Session.Get(ctx, r.URL.Query().Get("session_id"))
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 只有参与方可以读，避免任意账号枚举他人公钥
		isController := sess.ControllerID != "" && a.ownsDevice(ctx, acc.ID, sess.ControllerID)
		isTarget := sess.TargetID != "" && a.ownsDevice(ctx, acc.ID, sess.TargetID)
		if !isController && !isTarget {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "not a session party"))
			return
		}
		ctrlPub, _ := a.Store.GetDeviceIdentity(ctx, sess.ControllerID)
		targetPub, _ := a.Store.GetDeviceIdentity(ctx, sess.TargetID)
		httpx.OK(w, r, map[string]any{
			"session_id":        sess.ID,
			"controller_id":     sess.ControllerID,
			"target_id":         sess.TargetID,
			"controller_pubkey": ctrlPub,
			"target_pubkey":     targetPub,
		})
	})
	// ---- P0-4 邮箱登录（与手机号登录同构） ----
	mux.HandleFunc("/v1/auth/email/code", a.emailSendCode)
	mux.HandleFunc("/v1/auth/email/login", a.emailLogin)

	// ---- P0-1 文件传输 ----
	mux.HandleFunc("/v1/files", a.handleFiles)
	mux.HandleFunc("/v1/files/", a.handleFileItem)

	// ---- P0-3 协助码（临时授权） ----
	mux.HandleFunc("/v1/assist/create", a.createAssist)
	mux.HandleFunc("/v1/assist/redeem", a.redeemAssist)

	// ---- P1-1 客户端配置下发 ----
	mux.HandleFunc("/v1/config", a.clientConfig)

	// ---- P1-3 出口地址回显 ----
	mux.HandleFunc("/v1/net/reflect", a.netReflect)

	mux.HandleFunc("/v1/relay/ticket", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		sess, err := a.Session.Get(ctx, req.SessionID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		if sess.State != session.StateAuthorized && sess.State != session.StateActive {
			WriteErr(w, errcode.New(errcode.SessionAuthWindowExceeded, "session not authorized"))
			return
		}
		// 票据按「本端设备」签发：调用方必须证明自己是谁
		self, peer := "", ""
		if a.requireDevice(ctx, r, sess.ControllerID) == nil {
			self, peer = sess.ControllerID, sess.TargetID
		} else if a.requireDevice(ctx, r, sess.TargetID) == nil {
			self, peer = sess.TargetID, sess.ControllerID
		} else {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		token, _, err := a.Relay.Issue(sess.ID, self, peer)
		if err != nil {
			WriteErr(w, err)
			return
		}
		httpx.OK(w, r, map[string]any{"ticket": token, "self_device": self})
	})

	return httpx.Middleware(httpx.JSONMux(mux))
}

// ownsDevice 判断某个设备是否属于该账号。
func (a *App) ownsDevice(ctx context.Context, accountID, deviceID string) bool {
	d, err := a.Store.GetDevice(ctx, deviceID)
	if err != nil {
		return false
	}
	return d.AccountID == accountID
}

// isHex 判断字符串是否为小写/大写十六进制。
func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

// deviceFromCredential 用 X-Device-Token 解析出本次请求的主控设备。
//
// 返回的错误直接面向调用方：缺凭证或凭证不属于该账号时明确拒绝，
// 而不是悄悄挑一台设备顶替。
func (a *App) deviceFromCredential(ctx context.Context, r *http.Request, accountID string) (string, error) {
	token := r.Header.Get("X-Device-Token")
	if token == "" {
		return "", errcode.New(errcode.AdminForbidden, "controller device credential required")
	}
	devices, err := a.Device.List(ctx, accountID)
	if err != nil {
		return "", errcode.New(errcode.SrvInternal, err.Error())
	}
	for _, d := range devices {
		if a.Device.VerifyDeviceToken(ctx, d.ID, token) {
			return d.ID, nil
		}
	}
	return "", errcode.New(errcode.AdminForbidden, "controller device credential invalid")
}
