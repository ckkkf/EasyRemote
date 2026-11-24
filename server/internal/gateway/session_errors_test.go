package gateway

import (
	"context"
	"net/http"
	"testing"
)

// ADM-API-03h：网关按会话记录真实错误返回，详情页据此展示错误码时间线。
func TestSessionErrorsRecorded(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800011001")
	controller := f.loginAs("13800011002")
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
	// 未授权就 ready：状态机拒绝，并应记录一条会话级错误。
	if status := f.postJSON("/v1/sessions/ready", map[string]any{
		"session_id": connect.SessionID,
	}, map[string]string{"X-Device-Token": controller.DeviceToken}, nil); status == http.StatusOK {
		t.Fatal("ready on unauthorized session should fail")
	}
	errs, err := f.mem.ListSessionErrors(context.Background(), connect.SessionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 || errs[0].Name == "" || errs[0].Source != "sessions.ready" {
		t.Fatalf("errors: %+v", errs)
	}
}
