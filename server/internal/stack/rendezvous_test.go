package stack

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/easyremote/server/internal/device"
	"github.com/easyremote/server/internal/store"
)

// 信令必须与网关共用 store：这里直接验证「刚绑定的设备能通过 HELLO 认证」。
func TestRendezvousAcceptsRegisteredDevice(t *testing.T) {
	mem := store.NewMemory()
	ctx := context.Background()

	secret := []byte("0123456789abcdef0123")

	// 先登录（会创建账号），再把设备绑到该账号下
	authSvc, err := newAuthForTest(mem, secret)
	if err != nil {
		t.Fatal(err)
	}
	accToken, acc, err := authSvc.Login(ctx, "13800000001", "123456")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	devSvc := device.New(mem, secret)
	d, token, err := devSvc.Bind(ctx, acc.ID, "macos")
	if err != nil {
		t.Fatal(err)
	}

	h, err := BuildRendezvous(mem, secret)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hello := map[string]any{
		"type": 1,
		"body": map[string]any{
			"device_id":    d.ID,
			"auth_token":   accToken,
			"device_token": token,
		},
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env struct {
		Type int `json:"type"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Type != 2 {
		t.Fatalf("want HELLO_ACK(2), got %d", env.Type)
	}
}

// 无凭证必须被拒。
func TestRendezvousRejectsUnauthenticated(t *testing.T) {
	mem := store.NewMemory()
	h, err := BuildRendezvous(mem, []byte("0123456789abcdef0123"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]any{"type": 1, "body": map[string]any{"device_id": "dev-x"}})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		// 直接断开也算拒绝
		return
	}
	var env struct {
		Type int `json:"type"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Type != 12 {
		t.Fatalf("want DISCONNECT(12), got %d", env.Type)
	}
}

func newAuthForTest(mem store.Store, secret []byte) (*authService, error) {
	return newAuthService(mem, secret)
}
