package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// ADM-API-03c：按账号查会话（服务端过滤）+ 备注读写与审计。
func TestUserDetailSessionsAndNote(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	cs := Actor{Username: "cs1", Role: RoleCS}

	// 账号 acc1 两台设备，acc2 一台；会话跨两个账号。
	devices := []*store.Device{
		{ID: "d-a1", DeviceCode: "992000001", AccountID: "acc1", Platform: "macOS"},
		{ID: "d-a2", DeviceCode: "992000002", AccountID: "acc1", Platform: "Windows"},
		{ID: "d-b1", DeviceCode: "992000003", AccountID: "acc2", Platform: "macOS"},
	}
	for _, d := range devices {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := mem.CreateSession(ctxb, &store.Session{
		ID: "ses-a", ControllerID: "d-a1", TargetID: "d-a2", State: "closed", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateSession(ctxb, &store.Session{
		ID: "ses-b", ControllerID: "d-b1", TargetID: "d-a1", State: "requested", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListUserSessions(ctxb, cs, "acc1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 sessions for acc1, got %d", len(list))
	}
	if other, err := s.ListUserSessions(ctxb, cs, "acc2", 10); err != nil || len(other) != 1 {
		t.Fatalf("acc2 sessions: %v %+v", err, other)
	}

	// 备注：默认空，写入后能读回，且写审计。
	empty, err := s.GetUserNote(ctxb, cs, "acc1")
	if err != nil || empty.Note != "" {
		t.Fatalf("empty note: %v %+v", err, empty)
	}
	if err := s.SetUserNote(ctxb, cs, "acc1", "  疑似诈骗，已电话核实  "); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetUserNote(ctxb, cs, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "疑似诈骗，已电话核实" || got.UpdatedBy != "cs1" {
		t.Fatalf("note: %+v", got)
	}
	var noteAudit bool
	for _, a := range listAudit(mem) {
		if a.Action == "user.note" && a.Result == "ok" {
			noteAudit = true
		}
	}
	if !noteAudit {
		t.Fatal("缺 user.note 审计")
	}
	// 只读角色不能写备注。
	if err := s.SetUserNote(ctxb, Actor{Username: "ro", Role: RoleReadonly}, "acc1", "x"); !errcode.Is(err, errcode.AdminForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
