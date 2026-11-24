package gateway

import (
	"net/http"
	"testing"
)

// ER-905 现象 4：被控端拒绝必须是一次**成功的 HTTP 完成态**。
// 契约 GW-07 的 response 是 ConsentResult，errors 只有 CONN_DEVICE_NOT_FOUND / ADMIN_FORBIDDEN；
// 之前实现返回 4xx + CONN_CONSENT_REJECTED，主控端因此拿不到 closed 而一直等待。
func TestConsentRejectedReturnsCompletedClosedState(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800009001")
	controller := f.loginAs("13800009002")
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

	var consent struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	status := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": connect.SessionID,
		"accepted":   false,
		"reason":     "user_rejected",
	}, authHdr(target), &consent)
	if status != http.StatusOK {
		t.Fatalf("拒绝是完成态，HTTP 必须 200，got %d", status)
	}
	if consent.State != "closed" {
		t.Fatalf("want state=closed, got %q", consent.State)
	}

	var st struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if status := f.getJSON("/v1/connect/status?session_id="+connect.SessionID, authHdr(controller), &st); status != http.StatusOK {
		t.Fatalf("status code=%d", status)
	}
	if st.State != "closed" || st.Reason != "user_rejected" {
		t.Fatalf("主控轮询必须看到 closed/user_rejected，got %s/%q", st.State, st.Reason)
	}
}
