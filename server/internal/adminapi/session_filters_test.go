package adminapi

import (
	"context"
	"testing"
	"time"

	"github.com/easyremote/server/internal/store"
)

// ADM-API-03f1：会话筛选（时间范围/账号/设备）在存储层生效。
func TestSessionFilters(t *testing.T) {
	s, mem, _ := setup(t)
	ctxb := context.Background()
	act := Actor{Username: "ops", Role: RoleOps}
	base := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	devices := []*store.Device{
		{ID: "fa1", DeviceCode: "995000001", AccountID: "accA", Platform: "macOS"},
		{ID: "fa2", DeviceCode: "995000002", AccountID: "accA", Platform: "Windows"},
		{ID: "fb1", DeviceCode: "995000003", AccountID: "accB", Platform: "macOS"},
	}
	for _, d := range devices {
		if err := mem.CreateDevice(ctxb, d); err != nil {
			t.Fatal(err)
		}
	}
	sessions := []*store.Session{
		{ID: "s-old", ControllerID: "fa1", TargetID: "fa2", State: "closed", StartedAt: base},
		{ID: "s-new", ControllerID: "fa1", TargetID: "fb1", State: "requested", StartedAt: base.Add(48 * time.Hour)},
	}
	for _, se := range sessions {
		if err := mem.CreateSession(ctxb, se); err != nil {
			t.Fatal(err)
		}
	}

	// 时间范围：只要后半段。
	list, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, From: base.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "s-new" {
		t.Fatalf("time filter: %+v", list)
	}
	// 账号：accB 只参与 s-new。
	byAccount, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, AccountID: "accB"})
	if err != nil || len(byAccount) != 1 || byAccount[0].ID != "s-new" {
		t.Fatalf("account filter: %v %+v", err, byAccount)
	}
	// 设备：fb1 只参与 s-new。
	byDevice, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, DeviceID: "fb1"})
	if err != nil || len(byDevice) != 1 || byDevice[0].ID != "s-new" {
		t.Fatalf("device filter: %v %+v", err, byDevice)
	}
	// 组合筛选无结果。
	none, err := s.ListSessions(ctxb, act, "", store.Page{Limit: 10, AccountID: "accB", From: base})
	if err != nil {
		t.Fatal(err)
	}
	for _, se := range none {
		if se.ID == "s-old" {
			t.Fatalf("accB 不应包含 s-old: %+v", none)
		}
	}
}
