package gateway

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// ADM-API-02f2：建连尝试在进入 active 时被标成成功（成功率的分子来自真实状态迁移）。
func TestConnectAttemptMarksActive(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800010001")
	controller := f.loginAs("13800010002")
	f.prepareTarget(t, target)

	var connect struct {
		SessionID string `json:"session_id"`
	}
	if status := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "failclosed1",
	}, authHdr(controller), &connect); status != http.StatusOK {
		t.Fatalf("connect status=%d", status)
	}
	if status := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": connect.SessionID,
		"accepted":   true,
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("consent status=%d", status)
	}
	for _, party := range []*accountFixture{controller, target} {
		if status := f.postJSON("/v1/sessions/ready", map[string]any{
			"session_id": connect.SessionID,
		}, map[string]string{"X-Device-Token": party.DeviceToken}, nil); status != http.StatusOK {
			t.Fatalf("ready status=%d", status)
		}
	}
	total, active, err := f.mem.ConnectAttemptStats(context.Background(), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || active != 1 {
		t.Fatalf("attempt stats total=%d active=%d", total, active)
	}
}
