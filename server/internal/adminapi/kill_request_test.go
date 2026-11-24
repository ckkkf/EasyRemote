package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

func killRequestFixture(t *testing.T) (*Service, *store.Memory, string) {
	t.Helper()
	s, mem, sess := setup(t)
	ctxb := context.Background()
	for _, d := range []*store.Device{
		{ID: "dev-k1", DeviceCode: "991000001", AccountID: "acc1", Platform: "macOS"},
		{ID: "dev-k2", DeviceCode: "991000002", AccountID: "acc1", Platform: "Windows"},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	se, err := sess.Request(ctxb, "dev-k1", "dev-k2")
	if err != nil {
		t.Fatal(err)
	}
	return s, mem, se.ID
}

// ADM-API-07b1：双人确认全链路——同一人不能确认、第二人确认后才真正中断、重复确认失败。
func TestKillRequestTwoPersonFlow(t *testing.T) {
	s, mem, sessionID := killRequestFixture(t)
	ctxb := context.Background()
	ops := Actor{Username: "ops1", Role: RoleOps}
	super := Actor{Username: "root", Role: RoleSuper}

	row, err := s.RequestKill(ctxb, ops, sessionID, "用户报障")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "pending" || row.RequestedBy != "ops1" {
		t.Fatalf("row: %+v", row)
	}
	// 同一人确认必须被拒（否则等于单人操作）。
	if _, err := s.ConfirmKill(ctxb, ops, row.ID); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden for same actor, got %v", err)
	}
	// 第二人确认后会话才真正关闭。
	res, err := s.ConfirmKill(ctxb, super, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Closed {
		t.Fatalf("session not closed: %+v", res)
	}
	// 重复确认失败。
	if _, err := s.ConfirmKill(ctxb, super, row.ID); !errcode.Is(err, errcode.SessionExpired) {
		t.Fatalf("want expired on re-confirm, got %v", err)
	}
	// 已关闭会话不能再发起。
	if _, err := s.RequestKill(ctxb, ops, sessionID, "再来一次"); !errcode.Is(err, errcode.SessionExpired) {
		t.Fatalf("want expired for closed session, got %v", err)
	}
	// 审计里要有发起与确认两条。
	actions := map[string]bool{}
	for _, a := range listAudit(mem) {
		actions[a.Action] = true
	}
	if !actions["session.kill_request"] || !actions["session.kill_confirm"] || !actions["session.kill"] {
		t.Fatalf("audit actions: %+v", actions)
	}
}

// ADM-API-07b1：过期请求不能被确认（避免几小时前的批准被复用）。
func TestKillRequestExpires(t *testing.T) {
	s, _, sessionID := killRequestFixture(t)
	ctxb := context.Background()
	ops := Actor{Username: "ops1", Role: RoleOps}
	super := Actor{Username: "root", Role: RoleSuper}
	base := time.Now().UTC()
	s.SetClock(func() time.Time { return base })
	row, err := s.RequestKill(ctxb, ops, sessionID, "应急")
	if err != nil {
		t.Fatal(err)
	}
	// 6 分钟后（TTL 5 分钟）确认必须失败。
	s.SetClock(func() time.Time { return base.Add(6 * time.Minute) })
	if _, err := s.ConfirmKill(ctxb, super, row.ID); !errcode.Is(err, errcode.SessionExpired) {
		t.Fatalf("want expired, got %v", err)
	}
}
