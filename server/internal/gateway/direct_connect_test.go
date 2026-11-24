package gateway

import (
	"net/http"
	"testing"
)

type consentRecorder struct {
	sessionID, controllerID, targetID string
}

func (r *consentRecorder) NotifyConsentRequired(sessionID, controllerID, targetID string) error {
	r.sessionID, r.controllerID, r.targetID = sessionID, controllerID, targetID
	return nil
}

func TestSameAccountDirectOmitsPasswordButNotConsent(t *testing.T) {
	f := newFixture(t)
	host := f.acct
	controllerID, _, controllerToken := f.bindExtraDevice(host)
	f.app.Presence = fakePresence{online: true}
	notifier := &consentRecorder{}
	f.app.Notifier = notifier

	var result struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	status := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": host.DeviceCode,
		"direct":      true,
	}, map[string]string{
		"Authorization":  "Bearer " + host.Token,
		"X-Device-Token": controllerToken,
	}, &result)
	if status != http.StatusOK || result.State != "requested" || result.SessionID == "" {
		t.Fatalf("status=%d result=%+v", status, result)
	}
	if notifier.sessionID != result.SessionID || notifier.controllerID != controllerID ||
		notifier.targetID != host.DeviceID {
		t.Fatalf("wrong consent recipient: %+v", notifier)
	}
}

func TestCrossAccountDirectDoesNotCreateSessionWithoutPassword(t *testing.T) {
	f := newFixture(t)
	f.app.Presence = fakePresence{online: true}
	notifier := &consentRecorder{}
	f.app.Notifier = notifier
	host, controller := f.acct, f.loginAs("13800004002")
	status, raw := f.postJSONRaw("/v1/connect/request", map[string]any{
		"device_code": host.DeviceCode,
		"direct":      true,
	}, authHdr(controller))
	if status == http.StatusOK || errName(raw) != "CONN_CAPTCHA_EXPIRED" {
		t.Fatalf("status=%d envelope=%s", status, raw)
	}
	assertNoRequested(t, f)
	if notifier.sessionID != "" {
		t.Fatal("notified target despite missing password")
	}
}
