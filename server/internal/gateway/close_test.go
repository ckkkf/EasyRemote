package gateway

import (
	"net/http"
	"sync"
	"testing"
)

type fakeCloser struct {
	mu     sync.Mutex
	closed []struct{ sid, from, to, reason string }
}

func (f *fakeCloser) NotifySessionClosed(sid, from, to, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, struct{ sid, from, to, reason string }{sid, from, to, reason})
	return nil
}

// 任一端主动结束：写 closed 并向双方传播。
func TestClosePropagatesToBothPeers(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800008001")
	controller := f.loginAs("13800008002")
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
	if status := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": connect.SessionID,
		"accepted":   true,
	}, authHdr(target), nil); status != http.StatusOK {
		t.Fatalf("consent status=%d", status)
	}

	closer := &fakeCloser{}
	f.app.Closer = closer
	var closed struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if status := f.postJSON("/v1/sessions/close", map[string]any{
		"session_id": connect.SessionID,
		"reason":     "user hangup",
	}, authHdr(controller), &closed); status != http.StatusOK {
		t.Fatalf("close status=%d", status)
	}
	if closed.State != "closed" || closed.Reason != "user hangup" {
		t.Fatalf("close result=%+v", closed)
	}
	if len(closer.closed) != 1 {
		t.Fatalf("closer calls=%d", len(closer.closed))
	}
	got := closer.closed[0]
	if got.sid != connect.SessionID || got.from != controller.DeviceID || got.to != target.DeviceID {
		t.Fatalf("closer args=%+v", got)
	}
}
