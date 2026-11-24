package session

import (
	"context"
	"testing"
)

// ER-905 现象 4：被控端拒绝是一次**成功完成的本机决策**。
// 契约 GW-07 的 response 是 ConsentResult（errors 里没有 CONN_CONSENT_REJECTED），
// 因此 Consent 不该返回 error；权威状态必须落 closed 且 reason 可区分。
func TestConsentRejectedPersistsClosedAndReturnsContractResult(t *testing.T) {
	s, _ := setup(t)
	sess, err := s.Request(context.Background(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Consent(context.Background(), sess.ID, false, "user_rejected")
	if err != nil {
		t.Fatalf("拒绝属于契约完成态，不应返回 error，got %v", err)
	}
	if got == nil {
		t.Fatal("nil session")
	}
	if got.State != StateClosed {
		t.Fatalf("want closed, got %s", got.State)
	}
	if got.Reason != "user_rejected" {
		t.Fatalf("want reason user_rejected, got %q", got.Reason)
	}

	stored, err := s.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != StateClosed || stored.Reason != "user_rejected" {
		t.Fatalf("stored state/reason not converged: %s/%q", stored.State, stored.Reason)
	}
}

// ER-905 现象 4：状态查询必须带出关闭原因，客户端才能区分 rejected/timeout/peer close。
func TestClosedStatusCarriesReason(t *testing.T) {
	s, _ := setup(t)
	sess, err := s.Request(context.Background(), "c1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consent(context.Background(), sess.ID, false, "user_rejected"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason == "" {
		t.Fatal("closed 会话必须带 reason，否则主控无法显示「对方已拒绝」")
	}
}
