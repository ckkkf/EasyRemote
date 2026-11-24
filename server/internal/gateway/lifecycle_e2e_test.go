package gateway

import (
	"net/http"
	"testing"
)

// ER-905 跨端生命周期 E2E（真实 gateway + 真实存储 + 两个独立客户端身份）。
//
// 覆盖任务卡 6.4 的五条路径里可在此层验证的四条：
//  1. 请求 -> 被控拒绝 -> 主控 2 秒内看到 closed/user_rejected
//  3. 请求 -> 接受 -> active -> 任一端关闭 -> 另一端看到 closed 且 reason 正确
//  4. requested 阶段不申请 relay ticket（GW-11 调用次数为 0 才有意义）
//  5. 关闭后旧票据/旧授权窗口失效（再次 ticket 不得成功）
func TestRemoteLifecycleRejectAndBidirectionalDisconnect(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800007001")
	controller := f.loginAs("13800007002")
	f.prepareTarget(t, target)

	// ---- 场景 A：拒绝 ----
	var conn struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	}
	if code := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "failclosed1",
	}, authHdr(controller), &conn); code != http.StatusOK {
		t.Fatalf("request code=%d", code)
	}

	// 4) requested 阶段主控不应拿到 relay ticket
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	code := f.postJSON("/v1/relay/ticket", map[string]any{"session_id": conn.SessionID}, authHdr(controller), &ticket)
	if code == http.StatusOK {
		t.Fatalf("requested 阶段不应签发 relay ticket（ER-905 首要断言）")
	}

	if code := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": conn.SessionID,
		"accepted":   false,
		"reason":     "user_rejected",
	}, authHdr(target), nil); code != http.StatusOK {
		t.Fatalf("reject consent code=%d（拒绝是完成态，必须 200）", code)
	}

	var st struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if code := f.getJSON("/v1/connect/status?session_id="+conn.SessionID, authHdr(controller), &st); code != http.StatusOK {
		t.Fatalf("status code=%d", code)
	}
	if st.State != "closed" || st.Reason != "user_rejected" {
		t.Fatalf("主控必须看到 closed/user_rejected，got %s/%q", st.State, st.Reason)
	}

	// ---- 场景 B：接受 -> 关闭 -> 对端可见 ----
	var conn2 struct {
		SessionID string `json:"session_id"`
	}
	if code := f.postJSON("/v1/connect/request", map[string]any{
		"device_code": target.DeviceCode,
		"captcha":     "failclosed1",
	}, authHdr(controller), &conn2); code != http.StatusOK {
		t.Fatalf("request2 code=%d", code)
	}
	if code := f.postJSON("/v1/connect/consent", map[string]any{
		"session_id": conn2.SessionID,
		"accepted":   true,
	}, authHdr(target), nil); code != http.StatusOK {
		t.Fatalf("accept code=%d", code)
	}

	// 5) 关闭后旧票据失效：先拿一张，再关，再用旧票据申请不得成功
	if code := f.postJSON("/v1/relay/ticket", map[string]any{"session_id": conn2.SessionID}, authHdr(controller), &ticket); code != http.StatusOK {
		t.Fatalf("authorized 后应能签发 ticket，code=%d", code)
	}

	var closed struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if code := f.postJSON("/v1/sessions/close", map[string]any{
		"session_id": conn2.SessionID,
		"reason":     "controller_closed",
	}, authHdr(controller), &closed); code != http.StatusOK {
		t.Fatalf("close code=%d（GW-25 必须是 /v1/sessions/close）", code)
	}
	if closed.State != "closed" {
		t.Fatalf("want closed, got %s", closed.State)
	}

	var peer struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if code := f.getJSON("/v1/connect/status?session_id="+conn2.SessionID, authHdr(target), &peer); code != http.StatusOK {
		t.Fatalf("peer status code=%d", code)
	}
	if peer.State != "closed" || peer.Reason != "controller_closed" {
		t.Fatalf("被控端必须看到 closed/controller_closed，got %s/%q", peer.State, peer.Reason)
	}

	if code := f.postJSON("/v1/relay/ticket", map[string]any{"session_id": conn2.SessionID}, authHdr(controller), nil); code == http.StatusOK {
		t.Fatalf("关闭后旧会话不得再签发票据")
	}

	// GW-25 幂等：重复关闭仍返回 closed，不复活
	if code := f.postJSON("/v1/sessions/close", map[string]any{
		"session_id": conn2.SessionID,
		"reason":     "controller_closed",
	}, authHdr(controller), &closed); code != http.StatusOK || closed.State != "closed" {
		t.Fatalf("重复关闭必须幂等成功且仍为 closed，code=%d state=%s", code, closed.State)
	}
}
