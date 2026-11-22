package gateway

import (
	"net/http"
	"testing"
)

// ER-201：authorized 不计 active；双方 ready 后才 active。
func TestSessionActiveOnlyAfterBothPeersReady(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800007001")
	controller := f.loginAs("13800007002")
	outsider := f.loginAs("13800007003")
	f.prepareTarget(t, target)

	var connect struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	if status := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "failclosed1",
	}, authHdr(controller), &connect); status != http.StatusOK {
		t.Fatalf("connect status=%d", status)
	}
	if connect.State != "requested" {
		t.Fatalf("state=%s", connect.State)
	}
	if status := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": connect.SessionID,
		"accepted":   true,
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("consent status=%d", status)
	}

	var state struct {
		State string `json:"state"`
	}
	// 主控先 ready：仍 authorized。
	if status := f.postJSON("/v1/sessions/ready", map[string]any{
		"session_id": connect.SessionID,
	}, authHdr(controller), &state); status != http.StatusOK {
		t.Fatalf("controller ready status=%d", status)
	}
	if state.State != "authorized" {
		t.Fatalf("one-sided ready state=%s, want authorized", state.State)
	}

	// 被控端 ready：双方就绪 → active。
	if status := f.postJSON("/v1/sessions/ready", map[string]any{
		"session_id": connect.SessionID,
	}, authHdr(target), &state); status != http.StatusOK {
		t.Fatalf("target ready status=%d", status)
	}
	if state.State != "active" {
		t.Fatalf("both ready state=%s, want active", state.State)
	}

	// 非会话方不能上报 ready。
	if status := f.postJSON("/v1/sessions/ready", map[string]any{
		"session_id": connect.SessionID,
	}, authHdr(outsider), nil); status != http.StatusForbidden {
		t.Fatalf("outsider ready status=%d, want 403", status)
	}
}
