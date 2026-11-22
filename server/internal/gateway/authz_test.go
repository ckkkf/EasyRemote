package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func authHdr(a *accountFixture) map[string]string {
	return map[string]string{
		"Authorization":  "Bearer " + a.Token,
		"X-Device-Token": a.DeviceToken,
	}
}

func (f *fixture) getJSON(path string, headers map[string]string, out any) int {
	f.t.Helper()
	status, raw := f.do(http.MethodGet, path, nil, headers)
	if out != nil && status < 300 {
		if err := unmarshalData(raw, out); err != nil {
			f.t.Fatalf("decode GET %s: %v (body=%s)", path, err, raw)
		}
	}
	return status
}

// 跨账号/跨设备访问必须被拒绝：会话状态、公钥、同意、票据、文件。
func TestCrossDeviceAccessRejected(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800002001")
	controller := f.loginAs("13800002002")
	outsider := f.loginAs("13800002003")

	// 被控端设置临时密码，供跨账号主控发起请求。
	if status := f.postJSON("/v1/devices/password", map[string]any{
		"device_id": target.DeviceID,
		"password":  "crossdevicepw1",
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("set password status=%d", status)
	}

	var req struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	if status := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "crossdevicepw1",
	}, authHdr(controller), &req); status != http.StatusOK {
		t.Fatalf("connect request status=%d", status)
	}
	if req.SessionID == "" {
		t.Fatal("session id empty")
	}

	// 被控端本机同意（只有目标设备的 device 凭证可以）。
	if status := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": req.SessionID,
		"accepted":   true,
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("consent status=%d", status)
	}

	// 会话双方可以读状态；无关账号不行。
	var state struct {
		State string `json:"state"`
	}
	if status := f.getJSON("/v1/connect/status?session_id="+req.SessionID, authHdr(controller), &state); status != http.StatusOK {
		t.Fatalf("controller status=%d", status)
	}
	if state.State != "authorized" {
		t.Fatalf("state=%q, want authorized", state.State)
	}
	if status := f.getJSON("/v1/connect/status?session_id="+req.SessionID, authHdr(target), &state); status != http.StatusOK {
		t.Fatalf("target status=%d", status)
	}

	denied := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"status", http.MethodGet, "/v1/connect/status?session_id=" + req.SessionID, nil},
		{"keys", http.MethodGet, "/v1/connect/keys?session_id=" + req.SessionID, nil},
		{"consent", http.MethodPost, "/v1/connect/consent", map[string]any{"session_id": req.SessionID, "accepted": true}},
		{"ticket", http.MethodPost, "/v1/relay/ticket", map[string]any{"session_id": req.SessionID}},
	}
	for _, tc := range denied {
		var status int
		var raw []byte
		if tc.method == http.MethodGet {
			status, raw = f.do(http.MethodGet, tc.path, nil, authHdr(outsider))
		} else {
			buf, _ := json.Marshal(tc.body)
			status, raw = f.do(http.MethodPost, tc.path, bytes.NewReader(buf), map[string]string{
				"Authorization":  "Bearer " + outsider.Token,
				"X-Device-Token": outsider.DeviceToken,
				"Content-Type":   "application/json",
			})
		}
		if status != http.StatusForbidden {
			t.Fatalf("%s: status=%d, want 403 (body=%s)", tc.name, status, raw)
		}
		if name := errName(raw); name != "ADMIN_FORBIDDEN" {
			t.Fatalf("%s: name=%q, want ADMIN_FORBIDDEN", tc.name, name)
		}
	}

	// 文件：target 上传，无关账号不能下载/删除/取票。
	if status, raw := f.upload(target, "secret.txt", []byte("only-target")); status != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", status, raw)
	}
	files := f.listFiles(target)
	if len(files) == 0 {
		t.Fatal("target has no file")
	}
	fileID := files[0].ID
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{"download", http.MethodGet, "/v1/files/" + fileID},
		{"delete", http.MethodDelete, "/v1/files/" + fileID},
		{"ticket", http.MethodPost, "/v1/files/" + fileID + "/ticket"},
	} {
		status, raw := f.do(tc.method, tc.path, nil, authHdr(outsider))
		if status != http.StatusForbidden && status != http.StatusNotFound {
			t.Fatalf("file %s: status=%d, body=%s", tc.name, status, raw)
		}
	}
}

type fileRow struct {
	ID string `json:"id"`
}

func (f *fixture) listFiles(a *accountFixture) []fileRow {
	f.t.Helper()
	var out struct {
		Files []fileRow `json:"files"`
	}
	status := f.getJSON("/v1/files", authHdr(a), &out)
	if status != http.StatusOK {
		f.t.Fatalf("list files status=%d", status)
	}
	return out.Files
}
