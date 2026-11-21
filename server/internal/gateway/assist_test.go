package gateway

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// bindExtraDevice 在同一账号下再绑定一台设备（协助码需要「被控」与「主控」两台）。
func (f *fixture) bindExtraDevice(acct *accountFixture) (deviceID, deviceCode, deviceToken string) {
	f.t.Helper()
	var body map[string]any
	f.postJSON("/v1/devices/bind", map[string]any{"platform": "test-ctrl"},
		map[string]string{"Authorization": "Bearer " + acct.Token}, &body)
	return str(body["device_id"]), str(body["raw_code"]), str(body["device_token"])
}

func (f *fixture) createAssist(acct *accountFixture, deviceToken, deviceCode string) (int, []byte) {
	f.t.Helper()
	return f.postJSONRaw("/v1/assist/create", map[string]any{"device_code": deviceCode}, map[string]string{
		"Authorization":  "Bearer " + acct.Token,
		"X-Device-Token": deviceToken,
	})
}

func (f *fixture) redeemAssist(acct *accountFixture, deviceToken, code string) (int, []byte) {
	f.t.Helper()
	return f.postJSONRaw("/v1/assist/redeem", map[string]any{"code": code}, map[string]string{
		"Authorization":  "Bearer " + acct.Token,
		"X-Device-Token": deviceToken,
	})
}

// postJSONRaw 与 postJSON 相同但返回原始字节，便于断言错误码名字。
func (f *fixture) postJSONRaw(path string, payload any, headers map[string]string) (int, []byte) {
	f.t.Helper()
	var out map[string]any
	status := f.postJSON(path, payload, headers, &out)
	raw, _ := json.Marshal(out)
	return status, raw
}

// TestAssistCreateAndRedeem 覆盖主路径：被控生成 → 主控兑换 → 会话存在（requested，无需 captcha）。
func TestAssistCreateAndRedeem(t *testing.T) {
	f := newFixture(t)
	ctrlID, _, ctrlToken := f.bindExtraDevice(f.acct)

	status, raw := f.createAssist(f.acct, f.acct.DeviceToken, f.acct.DeviceCode)
	if status != 200 {
		t.Fatalf("assist create: %d %s", status, raw)
	}
	var created struct {
		Code    string `json:"code"`
		Expires string `json:"expires_at"`
		TTL     int    `json:"ttl_seconds"`
	}
	_ = unmarshalData(raw, &created)
	if len(created.Code) != 6 {
		t.Fatalf("assist code must be 6 digits, got %q", created.Code)
	}
	if created.TTL != 600 {
		t.Fatalf("ttl = %d, want 600", created.TTL)
	}

	status, raw = f.redeemAssist(f.acct, ctrlToken, created.Code)
	if status != 200 {
		t.Fatalf("assist redeem: %d %s", status, raw)
	}
	var redeemed struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
		TargetID  string `json:"target_id"`
	}
	_ = unmarshalData(raw, &redeemed)
	if redeemed.SessionID == "" || redeemed.State != "requested" {
		t.Fatalf("unexpected redeem result: %s", raw)
	}
	if redeemed.TargetID != f.acct.DeviceID {
		t.Fatalf("target = %s want %s", redeemed.TargetID, f.acct.DeviceID)
	}
	sess, err := f.mem.GetSession(t.Context(), redeemed.SessionID)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if sess.ControllerID != ctrlID || sess.TargetID != f.acct.DeviceID {
		t.Fatalf("unexpected session parties: %+v", sess)
	}
	if !containsAudit(t, f.mem, "assist.redeem", "ok") {
		t.Fatal("missing assist.redeem ok audit")
	}
}

// TestAssistRedeemTwiceFails 覆盖「二次兑换失败」。
func TestAssistRedeemTwiceFails(t *testing.T) {
	f := newFixture(t)
	_, _, ctrlToken := f.bindExtraDevice(f.acct)
	status, raw := f.createAssist(f.acct, f.acct.DeviceToken, f.acct.DeviceCode)
	if status != 200 {
		t.Fatalf("create: %d %s", status, raw)
	}
	var created struct {
		Code string `json:"code"`
	}
	_ = unmarshalData(raw, &created)
	if status, raw := f.redeemAssist(f.acct, ctrlToken, created.Code); status != 200 {
		t.Fatalf("first redeem: %d %s", status, raw)
	}
	status, raw = f.redeemAssist(f.acct, ctrlToken, created.Code)
	if status != http.StatusBadRequest {
		t.Fatalf("second redeem: want 400, got %d %s", status, raw)
	}
	if name := errName(raw); name != "CONN_ASSIST_CODE_USED" {
		t.Fatalf("want CONN_ASSIST_CODE_USED, got %s", name)
	}
	if !containsAudit(t, f.mem, "assist.redeem", "denied:used") {
		t.Fatal("missing denied:used audit")
	}
}

// TestAssistExpiredFails 覆盖「过期失败」。
func TestAssistExpiredFails(t *testing.T) {
	f := newFixture(t)
	_, _, ctrlToken := f.bindExtraDevice(f.acct)
	// 直接塞一条已过期的码，避免测试里 sleep 10 分钟。
	expired := "424242"
	rec := storeAssistCode(expired, f.acct.AccountID, f.acct.DeviceID, time.Now().Add(-time.Minute))
	if err := f.mem.PutAssistCode(t.Context(), &rec); err != nil {
		t.Fatalf("seed: %v", err)
	}
	status, raw := f.redeemAssist(f.acct, ctrlToken, expired)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", status, raw)
	}
	if name := errName(raw); name != "CONN_ASSIST_CODE_EXPIRED" {
		t.Fatalf("want CONN_ASSIST_CODE_EXPIRED, got %s", name)
	}
}

// TestAssistWrongDeviceCredentialForbidden 覆盖「错误设备凭证 403」：
// 拿另一台设备的凭证，不能替被控设备发码。
func TestAssistWrongDeviceCredentialForbidden(t *testing.T) {
	f := newFixture(t)
	_, _, ctrlToken := f.bindExtraDevice(f.acct)
	status, raw := f.createAssist(f.acct, ctrlToken, f.acct.DeviceCode)
	if status != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", status, raw)
	}
	// 未登记码也不能兑换。
	status, raw = f.redeemAssist(f.acct, ctrlToken, "000000")
	if status != http.StatusBadRequest || errName(raw) != "CONN_ASSIST_CODE_INVALID" {
		t.Fatalf("unknown code: want CONN_ASSIST_CODE_INVALID, got %d %s", status, raw)
	}
}

// TestAssistCrossAccountRedeem 是协助码的真实用途：被控方把码给外部主控。
func TestAssistCrossAccountRedeem(t *testing.T) {
	f := newFixture(t)
	helper := f.loginAs("13700003333")
	status, raw := f.createAssist(f.acct, f.acct.DeviceToken, f.acct.DeviceCode)
	if status != 200 {
		t.Fatalf("create: %d %s", status, raw)
	}
	var created struct {
		Code string `json:"code"`
	}
	_ = unmarshalData(raw, &created)
	status, raw = f.redeemAssist(helper, helper.DeviceToken, created.Code)
	if status != 200 {
		t.Fatalf("cross-account redeem: %d %s", status, raw)
	}
}
