package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/easyremote/server/internal/store"
)

type failNotifier struct{ err error }

func (f failNotifier) NotifyConsentRequired(string, string, string) error { return f.err }

type fakePresence struct {
	online bool
	err    error
}

func (f fakePresence) Online(context.Context, string) (bool, error) { return f.online, f.err }

func (f *fixture) requestConnect(t *testing.T, target, controller *accountFixture) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "failclosed1",
	})
	return f.do(http.MethodPost, "/v1/connect/request", bytes.NewReader(body), map[string]string{
		"Authorization":  "Bearer " + controller.Token,
		"X-Device-Token": controller.DeviceToken,
		"Content-Type":   "application/json",
	})
}

func (f *fixture) prepareTarget(t *testing.T, target *accountFixture) {
	t.Helper()
	if status := f.postJSON("/v1/devices/password", map[string]any{
		"device_id": target.DeviceID,
		"password":  "failclosed1",
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("set password status=%d", status)
	}
}

func assertNoRequested(t *testing.T, f *fixture) {
	t.Helper()
	sessions, err := f.mem.ListSessions(context.Background(), "requested", store.Page{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("requested sessions=%d, want 0", len(sessions))
	}
}

// TestConnectRequestFailsWhenTargetOffline：目标不在线时不能创建 requested 会话。
func TestConnectRequestFailsWhenTargetOffline(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800004001")
	controller := f.loginAs("13800004002")
	f.prepareTarget(t, target)
	f.app.Presence = fakePresence{online: false}

	status, raw := f.requestConnect(t, target, controller)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	if name := errName(raw); name != "CONN_DEVICE_OFFLINE" {
		t.Fatalf("name=%q, want CONN_DEVICE_OFFLINE", name)
	}
	assertNoRequested(t, f)
}

// TestConnectRequestFailsWhenNotifierUnavailable：生产注入 presence 但没有 notifier 时必须失败。
func TestConnectRequestFailsWhenNotifierUnavailable(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800004011")
	controller := f.loginAs("13800004012")
	f.prepareTarget(t, target)
	f.app.Presence = fakePresence{online: true}
	f.app.Notifier = nil

	status, raw := f.requestConnect(t, target, controller)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	if name := errName(raw); name != "CONN_DEVICE_OFFLINE" {
		t.Fatalf("name=%q", name)
	}
	assertNoRequested(t, f)
}

// TestConnectRequestFailsWhenDeliveryFails：通知投递失败必须收口，不能留 requested。
func TestConnectRequestFailsWhenDeliveryFails(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800004021")
	controller := f.loginAs("13800004022")
	f.prepareTarget(t, target)
	f.app.Presence = fakePresence{online: true}
	f.app.Notifier = failNotifier{err: errors.New("bus down")}

	status, raw := f.requestConnect(t, target, controller)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	if name := errName(raw); name != "CONN_DEVICE_OFFLINE" {
		t.Fatalf("name=%q", name)
	}
	assertNoRequested(t, f)
}

// 反向：presence 在线 + 通知成功时正常返回 requested（防止过度收紧）。
func TestConnectRequestSucceedsWhenOnlineAndNotified(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800004031")
	controller := f.loginAs("13800004032")
	f.prepareTarget(t, target)
	f.app.Presence = fakePresence{online: true}
	f.app.Notifier = failNotifier{err: nil}

	status, raw := f.requestConnect(t, target, controller)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	var out struct {
		State string `json:"state"`
	}
	if err := unmarshalData(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.State != "requested" {
		t.Fatalf("state=%q", out.State)
	}
}
