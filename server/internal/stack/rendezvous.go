package stack

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/store"
)

// envelope 是信令帧。
type envelope struct {
	Type int             `json:"type"`
	Body json.RawMessage `json:"body"`
}

type wsConn struct {
	c  *websocket.Conn
	mu chan struct{}
}

func newWSConn(c *websocket.Conn) *wsConn {
	w := &wsConn{c: c, mu: make(chan struct{}, 1)}
	w.mu <- struct{}{}
	return w
}

func (w *wsConn) Send(t int, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	<-w.mu
	defer func() { w.mu <- struct{}{} }()
	return w.c.WriteJSON(envelope{Type: t, Body: raw})
}

func (w *wsConn) Close() error { return w.c.Close() }

var rzUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// BuildRendezvous 用同一份存储构建信令 handler。
//
// 关键：认证要校验账号 token 与设备凭证，必须与网关共用 store，
// 否则会出现「刚在网关绑好的设备，信令侧说没授权」。
func BuildRendezvous(st store.Store, secret []byte) (http.Handler, error) {
	return BuildRendezvousWithHub(st, secret, nil)
}

// BuildRendezvousWithHub 用指定的 hub 构建信令 handler。
//
// 传入 hub 的目的：网关建会话后要能直接通知这个 hub（同进程部署），
// 否则被控端永远收不到 CONSENT_REQUIRED。
func BuildRendezvousWithHub(st store.Store, secret []byte, hub *signal.Hub) (http.Handler, error) {
	authSvc, err := auth.New(st, devSMS(), secret)
	if err != nil {
		return nil, err
	}
	devSvc := device.New(st, secret)
	if hub == nil {
		hub = signal.NewHub()
	}
	hub.SetInstanceID("stack")

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		raw, err := rzUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := newWSConn(raw)
		deviceID := ""

		defer func() {
			if deviceID != "" {
				hub.Unregister(deviceID, conn)
			}
			_ = raw.Close()
		}()

		for {
			_ = raw.SetReadDeadline(time.Now().Add(60 * time.Second))
			var env envelope
			if err := raw.ReadJSON(&env); err != nil {
				return
			}
			switch env.Type {
			case signal.TypeHello:
				var body struct {
					DeviceID    string `json:"device_id"`
					AuthToken   string `json:"auth_token"`
					DeviceToken string `json:"device_token"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if body.DeviceID == "" {
					return
				}
				if !authenticateDevice(authSvc, devSvc, st, body.DeviceID, body.AuthToken, body.DeviceToken) {
					_ = conn.Send(signal.TypeDisconnect, map[string]any{"reason": "unauthorized"})
					return
				}
				if old := hub.Register(body.DeviceID, conn); old != nil {
					_ = old.Close()
				}
				deviceID = body.DeviceID
				_ = conn.Send(signal.TypeHelloAck, map[string]any{
					"server_time": time.Now().UTC(), "heartbeat_interval_ms": 15000,
				})

			case signal.TypeQueryDevice:
				var body struct {
					DeviceID string `json:"device_id"`
				}
				_ = json.Unmarshal(env.Body, &body)
				_ = conn.Send(signal.TypeDeviceStatus, map[string]any{
					"device_id": body.DeviceID, "online": hub.Online(body.DeviceID),
				})

			case signal.TypeRequestConnect:
				var body struct {
					SessionID string `json:"session_id"`
					TargetID  string `json:"target_id"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if deviceID == "" || body.SessionID == "" || body.TargetID == "" {
					continue
				}
				_ = hub.NotifyConsentRequired(body.SessionID, deviceID, body.TargetID)

			case signal.TypeConsentResult:
				var body struct {
					SessionID string `json:"session_id"`
					Accepted  bool   `json:"accepted"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if deviceID == "" {
					continue
				}
				_ = hub.ForwardConsent(body.SessionID, deviceID, body.Accepted)

			case signal.TypeCandidates:
				var body struct {
					SessionID  string `json:"session_id"`
					Candidates []any  `json:"candidates"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if deviceID == "" {
					continue
				}
				_ = hub.ForwardCandidates(body.SessionID, deviceID, body.Candidates)
			}
		}
	})
	return mux, nil
}

func authenticateDevice(
	authSvc *auth.Service,
	devSvc *device.Service,
	st store.Store,
	deviceID, authToken, deviceToken string,
) bool {
	if authToken == "" || deviceToken == "" {
		return false
	}
	ctx := context.Background()
	acc, err := authSvc.VerifyToken(ctx, authToken)
	if err != nil {
		return false
	}
	d, err := st.GetDevice(ctx, deviceID)
	if err != nil || d.AccountID != acc.ID {
		return false
	}
	return devSvc.VerifyDeviceToken(ctx, deviceID, deviceToken)
}

// BuildRendezvousWithSecret 便于装配层传入密钥。
func BuildRendezvousWithSecret(st store.Store, secret []byte) (http.Handler, error) {
	return BuildRendezvous(st, secret)
}

// MountRendezvous 把信令 handler 挂到主 handler 的 /ws 路径。
func MountRendezvous(main http.Handler, rz http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			rz.ServeHTTP(w, r)
			return
		}
		main.ServeHTTP(w, r)
	})
}
