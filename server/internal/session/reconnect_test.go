package session

import (
	"testing"
	"time"
)

// 授权窗口内重连：同一对设备复用 authorized 会话，不重新弹授权。
func TestAuthorizedSessionReusedWithinWindow(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	first, _ := s.Request(ctx(), "c-reuse", "t-reuse")
	if _, err := s.Consent(ctx(), first.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	s.SetClock(func() time.Time { return now.Add(5 * time.Minute) })
	again, err := s.Request(ctx(), "c-reuse", "t-reuse")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.State != StateAuthorized {
		t.Fatalf("reconnect should reuse authorized session: %+v", again)
	}
}
