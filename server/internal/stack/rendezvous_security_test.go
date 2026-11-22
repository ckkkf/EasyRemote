package stack

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/signal"
)

type wsFixture struct {
	url         string
	deviceID    string
	deviceToken string
	authToken   string
}

func newWSFixture(t *testing.T) wsFixture {
	t.Helper()
	mem, h, err := NewDev()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := config.Secret()
	if err != nil {
		t.Fatal(err)
	}
	hub := signal.NewHub()
	hub.SetInstanceID("test")
	rz, err := BuildRendezvousWithHub(mem, secret, hub)
	if err != nil {
		t.Fatal(err)
	}
	h = MountRendezvous(h, rz)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	post := func(path string, body any, headers map[string]string) map[string]any {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var env struct {
			Data map[string]any `json:"data"`
		}
		_ = json.NewDecoder(res.Body).Decode(&env)
		return env.Data
	}

	login := post("/v1/auth/login", map[string]any{"phone": "13800006001", "code": "123456"}, nil)
	token, _ := login["token"].(string)
	bind := post("/v1/devices/bind", map[string]any{"platform": "test"}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	return wsFixture{
		url:         "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
		deviceID:    asString(bind["device_id"]),
		deviceToken: asString(bind["device_token"]),
		authToken:   token,
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func (f wsFixture) hello(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{
		"type": signal.TypeHello,
		"body": map[string]any{
			"device_id":    f.deviceID,
			"auth_token":   f.authToken,
			"device_token": f.deviceToken,
		},
	}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var ack struct {
		Type int `json:"type"`
	}
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("hello ack: %v", err)
	}
	if ack.Type != signal.TypeHelloAck {
		t.Fatalf("type=%d, want hello ack", ack.Type)
	}
}

// 非法 Origin 必须在 upgrade 阶段被拒绝（不再是 CheckOrigin=true）。
func TestRendezvousRejectsBadOrigin(t *testing.T) {
	t.Setenv("ER_ALLOWED_ORIGINS", "http://good.example")
	f := newWSFixture(t)
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	hdr := http.Header{}
	hdr.Set("Origin", "http://evil.example")
	conn, resp, err := dialer.Dial(f.url, hdr)
	if err == nil {
		_ = conn.Close()
		t.Fatal("bad origin must be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%v, want 403", resp)
	}
}

// 超过 64KB 的信令消息必须断开（SetReadLimit）。
func TestRendezvousRejectsOversizedMessage(t *testing.T) {
	f := newWSFixture(t)
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(f.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	f.hello(t, conn)

	big := strings.Repeat("x", signal.MaxWSMessageBytes+1024)
	writeErr := conn.WriteMessage(websocket.TextMessage, []byte(big))
	// 服务端可能先断开：写失败或随后读失败都证明超大消息被拒绝。
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := conn.ReadMessage()
	if writeErr == nil && readErr == nil {
		t.Fatal("oversized message must close the connection")
	}
}

// 未在 deadline 内认证必须断开。
func TestRendezvousAuthDeadline(t *testing.T) {
	t.Setenv("ER_WS_AUTH_TIMEOUT", "150ms")
	f := newWSFixture(t)
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(f.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unauthenticated connection must be closed after deadline")
	}
}
