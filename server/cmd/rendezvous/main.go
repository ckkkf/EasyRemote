// rendezvous 是信令长连接服务：在线状态、建连请求转发、候选交换。
// 本文件只做装配；路由与会话绑定逻辑在 internal/signal。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"

	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/stack"
)

type envelope struct {
	Type int             `json:"type"`
	Body json.RawMessage `json:"body"`
}

type wsConn struct {
	c  *websocket.Conn
	mu chan struct{} // 串行化写，避免并发写帧
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

var upgrader = signal.WSUpgrader(signal.AllowedOriginsFromEnv(os.Getenv("ER_ALLOWED_ORIGINS")))

func main() {
	addr := flag.String("addr", ":8082", "listen address")
	flag.Parse()

	secret, err := config.Secret()
	if err != nil {
		log.Fatalf("rendezvous: %v", err)
	}

	// 认证依赖：账号 token（auth）与设备凭证（device）都要能校验，
	// 因此 rendezvous 与 gateway 共用同一个 store 与密钥。
	st, err := stack.OpenStore(context.Background())
	if err != nil {
		log.Fatalf("rendezvous: open store: %v", err)
	}
	defer st.Close()
	devSvc := device.New(st, secret)
	authSvc, err := auth.New(st, stack.DevSMSProvider(), secret)
	if err != nil {
		log.Fatalf("rendezvous: %v", err)
	}
	authenticate := func(deviceID, authToken, deviceToken string) bool {
		if authToken == "" || deviceToken == "" {
			return false
		}
		acc, err := authSvc.VerifyToken(context.Background(), authToken)
		if err != nil {
			return false
		}
		d, err := st.GetDevice(context.Background(), deviceID)
		if err != nil || d.AccountID != acc.ID {
			return false
		}
		return devSvc.VerifyDeviceToken(context.Background(), deviceID, deviceToken)
	}

	hub := signal.NewHub()
	hub.SetInstanceID(*addr)

	// 有 REDIS_URL 时启用跨实例投递与共享在线表
	var bus *signal.RedisBus
	if url := os.Getenv("REDIS_URL"); url != "" {
		b, err := signal.NewRedisBus(context.Background(), url, *addr)
		if err != nil {
			log.Fatalf("rendezvous: redis bus: %v", err)
		}
		defer b.Close()
		bus = b
		hub.SetBus(b)
		if err := bus.Subscribe(context.Background(), hub.DeliverLocal); err != nil {
			log.Fatalf("rendezvous: subscribe: %v", err)
		}
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		d, s := hub.Counts()
		httpx.OK(w, r, struct {
			OK       bool   `json:"ok"`
			Service  string `json:"service"`
			Devices  int    `json:"devices"`
			Sessions int    `json:"sessions"`
		}{OK: true, Service: "rendezvous", Devices: d, Sessions: s})
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		raw, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := newWSConn(raw)
		// 单帧上限，防止单连接吃满内存（审查项 S12-5）
		raw.SetReadLimit(64 << 10)

		deviceID := ""
		defer func() {
			if deviceID != "" {
				hub.Unregister(deviceID, conn)
			}
			_ = conn.Close()
		}()

		for {
			// 每收到一帧就续期，避免「连接 5 分钟必断」（审查项 S12-3）
			_ = raw.SetReadDeadline(time.Now().Add(60 * time.Second))
			var env envelope
			if err := raw.ReadJSON(&env); err != nil {
				return
			}
			switch env.Type {
			case signal.TypeHello:
				var body struct {
					DeviceID  string `json:"device_id"`
					AuthToken string `json:"auth_token"`
					// DeviceToken 是网关绑定时下发的设备凭证
					DeviceToken string `json:"device_token"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if body.DeviceID == "" {
					return
				}
				// 认证：没有合法凭证不允许上线。
				// 否则任何人可用任意 deviceID 冒充，劫持授权与候选交换。
				if !authenticate(body.DeviceID, body.AuthToken, body.DeviceToken) {
					_ = conn.Send(signal.TypeDisconnect, map[string]any{"reason": "unauthorized"})
					return
				}
				if old := hub.Register(body.DeviceID, conn); old != nil {
					_ = old.Close() // 同设备重连：关掉旧连接
				}
				deviceID = body.DeviceID
				_ = conn.Send(signal.TypeHelloAck, map[string]any{
					"server_time": time.Now().UTC(), "heartbeat_interval_ms": 15000,
				})

			case signal.TypeHeartbeat:
				if deviceID != "" {
					hub.Touch(deviceID, conn)
				}

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
				if _, err := hub.Bind(body.SessionID, deviceID, body.TargetID); err != nil {
					_ = conn.Send(signal.TypeDisconnect, map[string]any{"session_id": body.SessionID, "reason": "bind_failed"})
					continue
				}
				if err := hub.NotifyConsentRequired(body.SessionID, deviceID, body.TargetID); err != nil {
					_ = conn.Send(signal.TypeDisconnect, map[string]any{"session_id": body.SessionID, "reason": "target_offline"})
				}

			case signal.TypeConsentResult:
				var body struct {
					SessionID string `json:"session_id"`
					Accepted  bool   `json:"accepted"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if err := hub.ForwardConsent(body.SessionID, deviceID, body.Accepted); err != nil {
					log.Printf("signal: consent rejected sid=%s dev=%s", body.SessionID, deviceID)
				}

			case signal.TypeCandidates:
				var body struct {
					SessionID  string `json:"session_id"`
					Candidates []any  `json:"candidates"`
				}
				_ = json.Unmarshal(env.Body, &body)
				if err := hub.ForwardCandidates(body.SessionID, deviceID, body.Candidates); err != nil {
					log.Printf("signal: candidates rejected sid=%s dev=%s", body.SessionID, deviceID)
				}

			case signal.TypeDisconnect:
				var body struct {
					SessionID string `json:"session_id"`
					Reason    string `json:"reason"`
				}
				_ = json.Unmarshal(env.Body, &body)
				_ = hub.Disconnect(body.SessionID, deviceID, body.Reason)
			}
		}
	})

	if err := serve.Run(serve.Config{Name: "rendezvous", Addr: *addr, Handler: mux, IdleTimeout: 120 * time.Second}); err != nil {
		log.Fatal(err)
	}
}
