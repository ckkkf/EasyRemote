package session

import (
	"sync"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ER-201：authorized 不算 active；只有双方数据面 ready 才进入 active。
func TestSessionActiveOnlyAfterBothPeersReady(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c-ready", "t-ready")
	authorized, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if authorized.State != StateAuthorized {
		t.Fatalf("after consent state=%s, want authorized", authorized.State)
	}

	one, err := s.Ready(ctx(), sess.ID, "c-ready")
	if err != nil {
		t.Fatal(err)
	}
	if one.State != StateAuthorized || !one.ReadyController || one.ReadyTarget {
		t.Fatalf("one-sided ready must stay authorized: %+v", one)
	}

	both, err := s.Ready(ctx(), sess.ID, "t-ready")
	if err != nil {
		t.Fatal(err)
	}
	if both.State != StateActive || !both.ReadyController || !both.ReadyTarget {
		t.Fatalf("both ready must activate: %+v", both)
	}

	// 重复 ready 幂等，不再改 version。
	again, err := s.Ready(ctx(), sess.ID, "t-ready")
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != both.Version || again.State != StateActive {
		t.Fatalf("duplicate ready mutated session: %+v", again)
	}
}

// 非会话方不能把自己标成 ready。
func TestReadyRejectsOutsider(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c-ready2", "t-ready2")
	if _, err := s.Consent(ctx(), sess.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ready(ctx(), sess.ID, "outsider"); err == nil {
		t.Fatal("outsider must not be able to mark ready")
	}
}

func TestIllegalTransitionRequestedToActive(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c-illegal", "t-illegal")
	before, _ := s.Get(ctx(), sess.ID)

	if _, err := s.Activate(ctx(), sess.ID); err == nil {
		t.Fatal("requested -> active must be rejected")
	}
	after, _ := s.Get(ctx(), sess.ID)
	if after.State != StateRequested || after.Version != before.Version {
		t.Fatalf("illegal transition changed state/version: %+v", after)
	}
}

func TestDuplicateConsentIdempotent(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c-dup", "t-dup")
	first, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Consent(ctx(), sess.ID, true, "")
	if err != nil {
		t.Fatalf("duplicate consent should be idempotent: %v", err)
	}
	if second.State != StateAuthorized || second.Version != first.Version {
		t.Fatalf("duplicate consent mutated state: first=%+v second=%+v", first, second)
	}
}

func TestCommandExpectedVersionConflict(t *testing.T) {
	s, _ := setup(t)
	sess, _ := s.Request(ctx(), "c-ver", "t-ver")
	if _, err := s.Apply(ctx(), Command{
		Kind:            CommandConsent,
		SessionID:       sess.ID,
		Accepted:        true,
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx(), Command{
		Kind:            CommandConsent,
		SessionID:       sess.ID,
		Accepted:        true,
		ExpectedVersion: 1,
	}); err == nil {
		t.Fatal("stale expected_version must be rejected")
	}
}

// 并发 accept/kill：CAS 必须保证只有一个写入生效，最终收敛到 closed。
func TestConcurrentAcceptAndKillCAS(t *testing.T) {
	s, _ := setup(t)
	for i := 0; i < 30; i++ {
		sess, err := s.Request(ctx(), "c-conc-"+itoa(i), "t-conc-"+itoa(i))
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.Consent(ctx(), sess.ID, true, "")
		}()
		go func() {
			defer wg.Done()
			_, _ = s.Kill(ctx(), sess.ID, "race")
		}()
		wg.Wait()

		got, err := s.Get(ctx(), sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != StateClosed {
			t.Fatalf("iteration %d: state=%s, want closed", i, got.State)
		}
		if got.Version < 2 {
			t.Fatalf("iteration %d: version=%d, want >=2", i, got.Version)
		}
	}
}

// 超时必须由主动迁移收口：过期后再 consent 会关闭会话。
func TestConsentTimeoutClosesViaCommand(t *testing.T) {
	s, _ := setup(t)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	sess, _ := s.Request(ctx(), "c-timeout", "t-timeout")
	s.SetClock(func() time.Time { return now.Add(ConsentTimeout + time.Second) })
	if _, err := s.Consent(ctx(), sess.ID, true, ""); !errcode.Is(err, errcode.ConnConsentTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	got, _ := s.Get(ctx(), sess.ID)
	if got.State != StateClosed || got.Reason != "consent_timeout" {
		t.Fatalf("timeout must close session: %+v", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

var _ = store.Page{}
