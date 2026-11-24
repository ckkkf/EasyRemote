package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-03d：解封与踢下线（原因必填、写审计、只影响目标账号）。
func TestUnbanAndKickUser(t *testing.T) {
	s, mem, sess := setup(t)
	ctxb := context.Background()
	super := Actor{Username: "root", Role: RoleSuper}

	if err := mem.CreateAccount(ctxb, &store.Account{ID: "acc-x", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*store.Device{
		{ID: "dx1", DeviceCode: "993000001", AccountID: "acc-x", Platform: "macOS", Online: true},
		{ID: "dx2", DeviceCode: "993000002", AccountID: "acc-x", Platform: "Windows", Online: true},
		{ID: "dy1", DeviceCode: "993000003", AccountID: "acc-y", Platform: "macOS", Online: true},
	} {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	// 账号 x 的一条会话，账号 y 的一条会话：踢下线只能关前者。
	xSess, err := sess.Request(ctxb, "dx1", "dx2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BanUser(ctxb, super, "acc-x", "违规"); err != nil {
		t.Fatal(err)
	}
	acc, _ := mem.GetAccount(ctxb, "acc-x")
	if acc.Status != "banned" {
		t.Fatalf("ban failed: %+v", acc)
	}
	// 解封：原因必填；解封后 active。
	if err := s.UnbanUser(ctxb, super, "acc-x", "  "); !errcode.Is(err, errcode.AdminReasonRequired) {
		t.Fatalf("want reason required, got %v", err)
	}
	if err := s.UnbanUser(ctxb, super, "acc-x", "误判"); err != nil {
		t.Fatal(err)
	}
	acc, _ = mem.GetAccount(ctxb, "acc-x")
	if acc.Status != "active" {
		t.Fatalf("unban failed: %+v", acc)
	}
	// 踢下线：关闭账号 x 的会话，设备置离线；账号 y 不受影响。
	if err := s.KickUser(ctxb, super, "acc-x", "疑似诈骗"); err != nil {
		t.Fatal(err)
	}
	got, _ := sess.Get(ctxb, xSess.ID)
	if got.State != session.StateClosed {
		t.Fatalf("session not closed: %+v", got)
	}
	d1, _ := mem.GetDevice(ctxb, "dx1")
	if d1.Online {
		t.Fatal("device should be offline after kick")
	}
	// 审计：user.unban / user.kick 都在。
	actions := map[string]bool{}
	for _, a := range listAudit(mem) {
		actions[a.Action] = true
	}
	if !actions["user.unban"] || !actions["user.kick"] {
		t.Fatalf("audit actions: %+v", actions)
	}
	// 只读角色被拒。
	if err := s.KickUser(ctxb, Actor{Username: "ro", Role: RoleReadonly}, "acc-x", "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	_ = time.Now
}
