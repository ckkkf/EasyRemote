package adminapi

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/store"
)

type fakePresence struct {
	online bool
}

func (f fakePresence) Online(context.Context, string) (bool, error) {
	return f.online, nil
}

// ER-800：后台在线数/设备列表必须读同一权威 presence，而不是设备表布尔值。
func TestAdminOnlineUsesPresence(t *testing.T) {
	s, mem, _ := setup(t)
	d := &store.Device{
		ID:         "dev-presence",
		DeviceCode: "123456789",
		AccountID:  "acc1",
		Platform:   "macos",
		Online:     true,
	}
	if err := mem.CreateDevice(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	act := Actor{Username: "cs", Role: RoleCS, IP: "10.0.0.1"}

	s.SetPresence(fakePresence{online: false})
	ov, err := s.Overview(context.Background(), act)
	if err != nil {
		t.Fatal(err)
	}
	if ov.DevicesOnline != 0 {
		t.Fatalf("presence=false online=%d, want 0", ov.DevicesOnline)
	}
	rows, err := s.ListDevices(context.Background(), act, store.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == d.ID && r.Online {
			t.Fatal("device row must use presence=false")
		}
	}

	s.SetPresence(fakePresence{online: true})
	ov, _ = s.Overview(context.Background(), act)
	if ov.DevicesOnline != 1 {
		t.Fatalf("presence=true online=%d, want 1", ov.DevicesOnline)
	}
}
