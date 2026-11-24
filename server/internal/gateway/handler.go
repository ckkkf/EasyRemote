// Package gateway 提供客户端 HTTP 处理器。cmd/gateway 只做装配。
package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/contract"
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

// Presence 是网关侧的在线判定接口（signal.Presence 实现）。
type Presence interface {
	Online(ctx context.Context, deviceID string) (bool, error)
}

// SessionCloser 把关闭事件传播给会话双方（signal.ConsentNotifier 实现）。
type SessionCloser interface {
	NotifySessionClosed(sessionID, from, to, reason string) error
}

// TicketEpochs 分配/读取票据代号（重连换票后旧票失效）。
type TicketEpochs interface {
	NextTicketEpoch(ctx context.Context, sessionID, deviceID string) (int64, error)
}

type App struct {
	Notifier ConsentNotifier
	// Presence 非空时，建会话前必须证明目标设备在线（生产网关强制注入）。
	Presence Presence
	// Closer 向双方传播关闭事件（生产注入 Bus）。
	Closer SessionCloser
	// TicketEpochs 为票据轮换代号的来源；为空时使用单机模式（epoch=0）。
	TicketEpochs TicketEpochs
	Limiter      ratelimit.Limiter
	Auth         *auth.Service
	Device       *device.Service
	Session      *session.Service
	Relay        *relaymgr.Service
	Store        store.Store
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
	// /readyz 的就绪检查由 stack.MountReady 注入；网关单独运行时退化为存活。
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, r, contract.OkResult{OK: true})
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
		httpx.OK(w, r, contract.OkResult{OK: true})
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
		httpx.OK(w, r, contract.LoginResult{Token: tok, AccountID: acc.ID})
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
		out := make([]contract.DeviceItem, 0, len(list))
		for _, d := range list {
			online := d.Online
			if a.Presence != nil {
				// 在线状态以信令租约为准，而不是绑定表里的历史布尔值。
				if v, perr := a.Presence.Online(ctx, d.ID); perr == nil {
					online = v
				} else {
					online = false
				}
			}
			out = append(out, contract.DeviceItem{
				DeviceCode: device.FormatCode(d.DeviceCode),
				RawCode:    d.DeviceCode,
				Platform:   d.Platform,
				Online:     online,
				LastSeen:   d.LastSeenAt,
			})
		}
		httpx.OK(w, r, contract.DeviceListResult{Devices: out})
	})

	mux.HandleFunc("/v1/devices/bind", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		acc, err := a.account(ctx, r)
		if err != nil {
			WriteErr(w, err)
			return
		}
		var req struct {
			Platform    string `json:"platform"`
			DeviceID    string `json:"device_id"`
			DeviceToken string `json:"device_token"`
			// 可选设备元信息（ADM-API-04a）：空值表示本次不上报，保留旧值。
			DeviceName    string `json:"device_name"`
			OSVersion     string `json:"os_version"`
			ClientVersion string `json:"client_version"`
			NATType       string `json:"nat_type"`
		}
		// 解析失败必须报错：以前这里 `_ = httpx.Decode` 吞掉错误、再用空值兜底成
		// "windows"，于是任何不带 platform（或带了未知字段被 DisallowUnknownFields
		// 拒掉）的调用都会把自己注册成 Windows 设备。
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body: "+err.Error()))
			return
		}
		// 带本机已有身份就复用同一台设备（幂等），否则新建。
		d, deviceToken, err := a.Device.BindOrReuse(
			ctx, acc.ID, normalizePlatform(req.Platform), req.DeviceID, req.DeviceToken,
		)
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 设备元信息随绑定上报；只有非空字段会被写入，避免某次漏报把已有信息清空。
		if req.DeviceName != "" || req.OSVersion != "" || req.ClientVersion != "" || req.NATType != "" {
			if err := a.Store.UpdateDeviceMeta(ctx, d.ID, req.DeviceName, req.OSVersion, req.ClientVersion, req.NATType); err != nil {
				WriteErr(w, errcode.New(errcode.SrvInternal, "save device meta: "+err.Error()))
				return
			}
			d.DeviceName, d.OSVersion, d.ClientVersion, d.NATType = req.DeviceName, req.OSVersion, req.ClientVersion, req.NATType
		}
		// device_token 只在此处返回一次，客户端须安全保存。
		// platform 回显服务端落库的真值，客户端不得再用自己上报的字符串显示。
		httpx.OK(w, r, contract.BindResult{
			DeviceCode:  device.FormatCode(d.DeviceCode),
			RawCode:     d.DeviceCode,
			DeviceID:    d.ID,
			DeviceToken: deviceToken,
			Platform:    d.Platform,
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
		httpx.OK(w, r, contract.VerifyCodeResult{Code: code, ExpiresAt: exp})
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
		httpx.OK(w, r, contract.OkResult{OK: true})
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
			// Direct 由客户端「同账号设备免密直连」设置置为 true。
			// 仅当目标设备与主控属于同一账号时才跳过临时密码；跨账号仍强制校验。
			Direct bool `json:"direct"`
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
		// 风控闸：命中黑名单的连接直接拒绝，并写一条告警（A10 / ADM-API-06c）。
		if subject, rerr := a.riskBlocked(ctx, acc.ID, target.DeviceCode, clientIP(r)); rerr != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "risk check failed"))
			return
		} else if subject != "" {
			if aerr := a.Store.RecordAlert(ctx, &store.RiskAlert{
				Kind:    "blacklist_block",
				Subject: subject,
				Detail:  "account=" + acc.ID + " device=" + target.DeviceCode,
			}); aerr != nil {
				WriteErr(w, errcode.New(errcode.SrvInternal, "risk alert write failed"))
				return
			}
			WriteErr(w, errcode.New(errcode.ConnRateLimited, "blocked by risk control"))
			return
		}
		sameAccount := target.AccountID == acc.ID
		trusted := a.Device.HasTrust(ctx, controllerID, target.ID)
		// 同账号免密直连：账号本身就是最强的身份证明，同一账号下的设备
		// 不需要再向自己输入对方屏幕上的临时密码。
		directSameAccount := req.Direct && sameAccount
		if !trusted && !directSameAccount {
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
		// 建会话前证明目标在线：不能返回一个永远没人处理的 requested。
		if a.Presence != nil {
			online, perr := a.Presence.Online(ctx, target.ID)
			if perr != nil || !online {
				WriteErr(w, errcode.New(errcode.ConnDeviceOffline, "target offline"))
				return
			}
		}
		sess, err := a.Session.Request(ctx, controllerID, target.ID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 建连埋点（ADM-API-02f2）：每次成功创建会话记一次尝试；进入 active 时再标成功。
		if aerr := a.Store.CreateConnectAttempt(ctx, &store.ConnectAttempt{
			SessionID:    sess.ID,
			AccountID:    acc.ID,
			ControllerID: controllerID,
			TargetID:     target.ID,
		}); aerr != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "connect attempt write failed"))
			return
		}
		// 通知被控端有新的授权请求；生产（注入 Presence）下投递失败必须 fail-closed。
		if sess.State == session.StateRequested {
			if a.Notifier == nil {
				if a.Presence != nil {
					_, _ = a.Session.Close(ctx, sess.ID, "no consent notifier")
					WriteErr(w, errcode.New(errcode.ConnDeviceOffline, "consent notifier unavailable"))
					return
				}
			} else if nerr := a.Notifier.NotifyConsentRequired(sess.ID, controllerID, target.ID); nerr != nil && a.Presence != nil {
				_, _ = a.Session.Close(ctx, sess.ID, "consent delivery failed")
				WriteErr(w, errcode.New(errcode.ConnDeviceOffline, "consent delivery failed"))
				return
			}
		}
		httpx.OK(w, r, contract.ConnectResult{
			SessionID: sess.ID,
			State:     sess.State,
			Trusted:   trusted || directSameAccount,
		})
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
		httpx.OK(w, r, contract.ConsentResult{SessionID: sess.ID, State: sess.State, AuthExpireAt: sess.AuthExpireAt})
	})

	mux.HandleFunc("/v1/connect/status", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// 契约 auth: bearer —— 先证明账号身份，再校验该账号拥有会话的一方。
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
		if !a.ownsDevice(ctx, acc.ID, sess.ControllerID) && !a.ownsDevice(ctx, acc.ID, sess.TargetID) {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party account required"))
			return
		}
		httpx.OK(w, r, contract.SessionStateResult{SessionID: sess.ID, State: sess.State, Reason: sess.Reason})
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
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	// 下发会话双方的身份公钥：只有会话两端能读，且只对活跃会话有效。
	// 会话内任一端主动结束：写 closed，并向双方传播关闭事件（客户端也会轮询状态兜底）。
	mux.HandleFunc("/v1/sessions/close", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			SessionID string `json:"session_id"`
			Reason    string `json:"reason"`
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
		deviceID := ""
		if a.requireDevice(ctx, r, sess.ControllerID) == nil {
			deviceID = sess.ControllerID
		} else if a.requireDevice(ctx, r, sess.TargetID) == nil {
			deviceID = sess.TargetID
		} else {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "closed by " + deviceID
		}
		closed, err := a.Session.Close(ctx, req.SessionID, reason)
		if err != nil {
			WriteErr(w, err)
			return
		}
		if a.Closer != nil {
			// 状态已 closed；传播失败不能回滚，客户端状态轮询仍会收敛。
			_ = a.Closer.NotifySessionClosed(closed.ID, closed.ControllerID, closed.TargetID, reason)
		}
		httpx.OK(w, r, contract.SessionStateResult{
			SessionID: closed.ID,
			State:     closed.State,
			Reason:    closed.Reason,
		})
	})

	// 数据面 ready：会话双方各自上报；两端都 ready 且已授权才进入 active。
	// GW-27：客户端健康事件上报（ADM-API-02f5）。只有会话双方设备可以上报。
	mux.HandleFunc("/v1/sessions/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			SessionID string `json:"session_id"`
			Kind      string `json:"kind"`
		}
		if err := httpx.Decode(w, r, &req); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
			return
		}
		switch req.Kind {
		case "crash", "reconnect_attempt", "reconnect_success":
		default:
			WriteErr(w, errcode.New(errcode.SrvInternal, "unknown kind: "+req.Kind))
			return
		}
		sess, err := a.Session.Get(ctx, req.SessionID)
		if err != nil {
			WriteErr(w, err)
			return
		}
		deviceID := ""
		if a.requireDevice(ctx, r, sess.ControllerID) == nil {
			deviceID = sess.ControllerID
		} else if a.requireDevice(ctx, r, sess.TargetID) == nil {
			deviceID = sess.TargetID
		} else {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		if err := a.Store.RecordHealthEvent(ctx, &store.HealthEvent{
			SessionID: req.SessionID, DeviceID: deviceID, Kind: req.Kind,
		}); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "store health event: "+err.Error()))
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	// GW-26：会话质量采样上报（ADM-API-02c）。只有会话双方设备可以上报。
	mux.HandleFunc("/v1/sessions/quality", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req struct {
			SessionID string `json:"session_id"`
			Samples   []struct {
				TS           time.Time `json:"ts"`
				LatencyMs    int       `json:"latency_ms"`
				LossPermille int       `json:"loss_permille"`
				Fps          int       `json:"fps"`
				Tier         string    `json:"tier"`
				Relay        bool      `json:"relay"`
				Link         string    `json:"link"`
				TargetFps    int       `json:"target_fps"`
			} `json:"samples"`
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
		if a.requireDevice(ctx, r, sess.ControllerID) != nil && a.requireDevice(ctx, r, sess.TargetID) != nil {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		if len(req.Samples) == 0 {
			WriteErr(w, errcode.New(errcode.SrvInternal, "empty samples"))
			return
		}
		samples := make([]store.QualitySample, 0, len(req.Samples))
		for _, s := range req.Samples {
			ts := s.TS
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			// link 只接受 p2p/relay（其它值按未上报处理）；target_fps 限制在 0-240。
			link := strings.ToLower(strings.TrimSpace(s.Link))
			if link != "p2p" && link != "relay" {
				link = ""
			}
			targetFps := s.TargetFps
			if targetFps < 0 || targetFps > 240 {
				targetFps = 0
			}
			samples = append(samples, store.QualitySample{
				SessionID:    req.SessionID,
				TS:           ts.UTC().Truncate(time.Second),
				LatencyMs:    s.LatencyMs,
				LossPermille: s.LossPermille,
				Fps:          s.Fps,
				Tier:         s.Tier,
				Relay:        s.Relay,
				Link:         link,
				TargetFps:    targetFps,
			})
		}
		if err := a.Store.AppendQualitySamples(ctx, samples); err != nil {
			WriteErr(w, errcode.New(errcode.SrvInternal, "store quality: "+err.Error()))
			return
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})

	mux.HandleFunc("/v1/sessions/ready", func(w http.ResponseWriter, r *http.Request) {
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
		deviceID := ""
		if a.requireDevice(ctx, r, sess.ControllerID) == nil {
			deviceID = sess.ControllerID
		} else if a.requireDevice(ctx, r, sess.TargetID) == nil {
			deviceID = sess.TargetID
		} else {
			WriteErr(w, errcode.New(errcode.AdminForbidden, "session party credential required"))
			return
		}
		updated, err := a.Session.Ready(ctx, req.SessionID, deviceID)
		if err != nil {
			a.recordSessionError(ctx, req.SessionID, "sessions.ready", err)
			WriteErr(w, err)
			return
		}
		// 进入 active 才算建连成功（ADM-API-02f2）；失败不阻断响应，但要能被统计发现。
		if updated.State == session.StateActive {
			if merr := a.Store.MarkConnectAttemptActive(ctx, updated.ID); merr != nil && !errors.Is(merr, store.ErrNotFound) {
				WriteErr(w, errcode.New(errcode.SrvInternal, "connect attempt update failed"))
				return
			}
		}
		httpx.OK(w, r, contract.SessionStateResult{
			SessionID: updated.ID,
			State:     updated.State,
			Reason:    updated.Reason,
		})
	})

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
		httpx.OK(w, r, contract.SessionKeysResult{
			SessionID:        sess.ID,
			ControllerID:     sess.ControllerID,
			TargetID:         sess.TargetID,
			ControllerPubkey: ctrlPub,
			TargetPubkey:     targetPub,
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
			err := errcode.New(errcode.SessionAuthWindowExceeded, "session not authorized")
			a.recordSessionError(ctx, sess.ID, "relay.ticket", err)
			WriteErr(w, err)
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
		var epoch int64
		if a.TicketEpochs != nil {
			var eerr error
			epoch, eerr = a.TicketEpochs.NextTicketEpoch(ctx, sess.ID, self)
			if eerr != nil {
				// 轮换后端不可用时不能签发旧代号票据。
				WriteErr(w, errcode.New(errcode.SrvInternal, "ticket epoch unavailable"))
				return
			}
		}
		token, _, err := a.Relay.IssueWithEpoch(sess.ID, self, peer, epoch)
		if err != nil {
			WriteErr(w, err)
			return
		}
		// 签发中继票据说明这次连接走 relay（ADM-API-02f2）；p2p 需要客户端上报，服务端不猜。
		if merr := a.Store.MarkConnectAttemptRelay(ctx, sess.ID); merr != nil && !errors.Is(merr, store.ErrNotFound) {
			WriteErr(w, errcode.New(errcode.SrvInternal, "connect attempt update failed"))
			return
		}
		httpx.OK(w, r, contract.RelayTicketResult{Ticket: token, SelfDevice: self})
	})

	return httpx.Middleware(httpx.MethodGuard(routeSpecs, httpx.JSONMux(mux)))
}

// ownsDevice 判断某个设备是否属于该账号。
func (a *App) ownsDevice(ctx context.Context, accountID, deviceID string) bool {
	d, err := a.Store.GetDevice(ctx, deviceID)
	if err != nil {
		return false
	}
	return d.AccountID == accountID
}

// normalizePlatform 归一化客户端上报的平台名。
//
// 空值/纯空白一律记成 "unknown"：**绝不猜成 windows**。历史上这里默认
// "windows"，任何没带 platform 的调用都会变成一台 Windows 设备，客户端列表里
// 就会出现「macOS 机器显示成 Windows 设备」。大小写也在这里统一，避免同一台
// 机器先后以 macOS/macos 两种写法出现。
func normalizePlatform(raw string) string {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return "unknown"
	}
	return p
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
